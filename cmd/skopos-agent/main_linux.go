//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"golang.org/x/sys/unix"
	"github.com/raghavtk/skopos/internal/model"
)

type kernelEvent struct {
	MonoNS       uint64
	SocketCookie uint64
	PID          uint32
	Type         uint32
	SourceIP     uint32
	DestIP       uint32
	SourcePort   uint16
	DestPort     uint16
	Comm         [16]byte
}

func main() {
	object := flag.String("bpf-object", "bpf/skopos.bpf.o", "compiled BPF ELF object")
	endpoint := flag.String("server", "http://127.0.0.1:8080", "Skopos control plane")
	token := flag.String("token", os.Getenv("SKOPOS_TOKEN"), "ingest bearer token")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	spec, err := ebpf.LoadCollectionSpec(*object)
	if err != nil {
		log.Fatal(err)
	}
	objects, err := ebpf.NewCollection(spec)
	if err != nil {
		log.Fatal(err)
	}
	defer objects.Close()
	retransmit, err := link.Kprobe("tcp_retransmit_skb", objects.Programs["trace_retransmit"], nil)
	if err != nil {
		log.Fatal(err)
	}
	defer retransmit.Close()
	exit, err := link.Tracepoint("sched", "sched_process_exit", objects.Programs["trace_exit"], nil)
	if err != nil {
		log.Fatal(err)
	}
	defer exit.Close()
	reader, err := ringbuf.NewReader(objects.Maps["events"])
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()
	go func() { <-ctx.Done(); reader.Close() }()
	node, _ := os.Hostname()
	boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
	bootID := strings.TrimSpace(string(boot))
	// Calibrate CLOCK_MONOTONIC to wall time at startup. The control plane
	// retains monotonic time and boot ID for later drift-aware ordering.
	var mono unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &mono); err != nil {
		log.Fatal(err)
	}
	bootWall := time.Now().Add(-time.Duration(mono.Sec)*time.Second - time.Duration(mono.Nsec))
	queue := make(chan model.Event, 4096)
	var dropped uint64
	go func() {
		for {
			rec, err := reader.Read()
			if err != nil {
				if !errors.Is(err, ringbuf.ErrClosed) {
					log.Printf("ring read: %v", err)
				}
				close(queue)
				return
			}
			var k kernelEvent
			if err := binary.Read(bytes.NewReader(rec.RawSample), binary.LittleEndian, &k); err != nil {
				log.Printf("decode: %v", err)
				continue
			}
			e := convert(k, node, bootID, bootWall)
			select {
			case queue <- e:
			default:
				atomic.AddUint64(&dropped, 1)
			}
		}
	}()
	batch := make([]model.Event, 0, 200)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := post(*endpoint, *token, batch); err != nil {
			log.Printf("ingest failed, %d events lost: %v", len(batch), err)
			atomic.AddUint64(&dropped, uint64(len(batch)))
		}
		batch = batch[:0]
	}
	for {
		select {
		case e, ok := <-queue:
			if !ok {
				flush()
				log.Printf("dropped=%d", atomic.LoadUint64(&dropped))
				return
			}
			batch = append(batch, e)
			if len(batch) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
			if n := atomic.SwapUint64(&dropped, 0); n > 0 {
				log.Printf("dropped_events=%d", n)
			}
		}
	}
}

func convert(k kernelEvent, node, bootID string, bootWall time.Time) model.Event {
	name := strings.TrimRight(string(k.Comm[:]), "\x00")
	e := model.Event{Version: 1, ID: fmt.Sprintf("%s-%d-%d-%d-%d", bootID, k.MonoNS, k.PID, k.Type, k.SocketCookie), Time: bootWall.Add(time.Duration(k.MonoNS)).UTC(), MonoNS: int64(k.MonoNS), BootID: bootID, Node: node, SocketID: fmt.Sprintf("%d", k.SocketCookie), SourceIP: ip(k.SourceIP), DestIP: ip(k.DestIP), SourcePort: int(k.SourcePort), DestPort: int(k.DestPort), Attributes: map[string]string{"current_comm": name, "current_pid": fmt.Sprint(k.PID)}}
	if k.Type == 1 {
		e.Type = "tcp_retransmit"
	} else {
		e.Type = "process_exit"
		e.PID = int(k.PID)
		e.Reason = "process exited"
		e.SocketID = ""
	}
	return e
}

func ip(v uint32) string {
	if v == 0 {
		return ""
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return net.IP(b).String()
}

func post(base, token string, batch []model.Event) error {
	b, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/v1/events", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return nil
}
