package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/raghavtk/skopos/internal/model"
	"github.com/raghavtk/skopos/internal/server"
	"github.com/raghavtk/skopos/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "demo":
		demo(os.Args[2:])
	case "request", "otlp", "topology", "events", "status":
		query(os.Args[1], os.Args[2:])
	case "ingest":
		ingest(os.Args[2:])
	case "export":
		export(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: skopos <serve|demo|ingest|request|otlp|export|topology|events|status> [flags]")
}

func serve(args []string) {
	f := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := f.String("listen", "127.0.0.1:8080", "HTTP listen address")
	data := f.String("data", "data/events.jsonl", "event WAL path")
	token := f.String("token", os.Getenv("SKOPOS_TOKEN"), "ingest bearer token")
	f.Parse(args)
	s, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Printf("Skopos listening on %s with %d events", *listen, len(s.Events()))
	log.Fatal(http.ListenAndServe(*listen, server.Server{Store: s, Token: *token}.Handler()))
}

func base(f *flag.FlagSet) *string {
	return f.String("server", "http://127.0.0.1:8080", "Skopos server")
}

func query(cmd string, args []string) {
	f := flag.NewFlagSet(cmd, flag.ExitOnError)
	serverURL := base(f)
	f.Parse(args)
	path := "/v1/health"
	switch cmd {
	case "request", "otlp":
		if f.NArg() != 1 {
			log.Fatal(cmd + " requires an ID")
		}
		path = "/v1/requests/" + url.PathEscape(f.Arg(0))
		if cmd == "otlp" {
			path += "/otlp"
		}
	case "topology":
		path = "/v1/topology"
	case "events":
		path = "/v1/events"
	}
	r, err := http.Get(strings.TrimRight(*serverURL, "/") + path)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		log.Fatalf("%s: %s", r.Status, b)
	}
	io.Copy(os.Stdout, r.Body)
}

func ingest(args []string) {
	f := flag.NewFlagSet("ingest", flag.ExitOnError)
	serverURL := base(f)
	file := f.String("file", "", "JSON array of events")
	token := f.String("token", os.Getenv("SKOPOS_TOKEN"), "ingest bearer token")
	f.Parse(args)
	if *file == "" {
		log.Fatal("ingest requires -file")
	}
	b, err := os.ReadFile(*file)
	if err != nil {
		log.Fatal(err)
	}
	post(*serverURL, *token, b)
}

func export(args []string) {
	f := flag.NewFlagSet("export", flag.ExitOnError)
	serverURL := base(f)
	destination := f.String("otlp", "http://127.0.0.1:4318/v1/traces", "OTLP/HTTP JSON endpoint")
	f.Parse(args)
	if f.NArg() != 1 {
		log.Fatal("export requires request ID")
	}
	client := http.Client{Timeout: 15 * time.Second}
	r, err := client.Get(strings.TrimRight(*serverURL, "/") + "/v1/requests/" + url.PathEscape(f.Arg(0)) + "/otlp")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		log.Fatalf("request export failed: %s", r.Status)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		log.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, *destination, bytes.NewReader(b))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("OTLP endpoint returned %s: %s", resp.Status, body)
	}
	fmt.Println("exported request", f.Arg(0))
}

func post(serverURL, token string, b []byte) {
	r, err := http.NewRequest(http.MethodPost, strings.TrimRight(serverURL, "/")+"/v1/events", bytes.NewReader(b))
	if err != nil {
		log.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		log.Fatalf("%s: %s", resp.Status, body)
	}
	fmt.Print(string(body))
}

func demo(args []string) {
	f := flag.NewFlagSet("demo", flag.ExitOnError)
	serverURL := base(f)
	token := f.String("token", os.Getenv("SKOPOS_TOKEN"), "ingest bearer token")
	f.Parse(args)
	events := demoEvents()
	b, err := json.Marshal(events)
	if err != nil {
		log.Fatal(err)
	}
	post(*serverURL, *token, b)
	fmt.Println("Inspect: skopos request abc123")
}

func demoEvents() []model.Event {
	t := time.Now().UTC().Truncate(time.Millisecond)
	return []model.Event{
		{Version: 1, ID: "demo-http-1", Type: "http", Time: t, Node: "node-a", Pod: "frontend-1", Service: "frontend", Peer: "payments", SocketID: "socket-42", RequestID: "abc123", Method: "GET", Path: "/checkout", Status: 200, DurationMS: 480},
		{Version: 1, ID: "demo-dns-1", Type: "dns", Time: t.Add(8 * time.Millisecond), Node: "node-a", Pod: "frontend-1", SocketID: "socket-42", DNSName: "payments.default.svc.cluster.local", DNSCode: "NOERROR"},
		{Version: 1, ID: "demo-tcp-1", Type: "tcp_retransmit", Time: t.Add(95 * time.Millisecond), Node: "node-a", SocketID: "socket-42", Peer: "payments"},
		{Version: 1, ID: "demo-tcp-2", Type: "tcp_retransmit", Time: t.Add(155 * time.Millisecond), Node: "node-a", SocketID: "socket-42", Peer: "payments"},
		{Version: 1, ID: "demo-tcp-3", Type: "tcp_retransmit", Time: t.Add(230 * time.Millisecond), Node: "node-a", SocketID: "socket-42", Peer: "payments"},
		{Version: 1, ID: "demo-pod-1", Type: "pod", Time: t.Add(250 * time.Millisecond), Node: "node-b", Pod: "payments", Reason: "terminated and rescheduled"},
	}
}
