package correlate

import (
	"fmt"
	"testing"
	"time"

	"github.com/raghavtk/skopos/internal/model"
)

func TestRequestEvidenceUsesSocketIdentity(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := []model.Event{
		{Version: 1, ID: "http", Type: "http", Time: start, RequestID: "r1", Service: "frontend", Peer: "payments", SocketID: "s1", DurationMS: 480, Status: 200},
		{Version: 1, ID: "retransmit", Type: "tcp_retransmit", Time: start.Add(100 * time.Millisecond), SocketID: "s1"},
		{Version: 1, ID: "unrelated", Type: "tcp_retransmit", Time: start.Add(110 * time.Millisecond), SocketID: "s2"},
	}
	r, ok := Build(events, "r1")
	if !ok {
		t.Fatal("request not found")
	}
	if len(r.Evidence) != 2 {
		t.Fatalf("wanted 2 evidence items; got %d", len(r.Evidence))
	}
	if len(r.Diagnoses) == 0 || r.Diagnoses[0].Cause != "tcp_retransmissions" {
		t.Fatalf("missing retransmission diagnosis: %+v", r.Diagnoses)
	}
	if r.DurationMS != 480 {
		t.Fatalf("wrong duration: %v", r.DurationMS)
	}
}

func benchmarkReconstruct(b *testing.B, services int) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	events := make([]model.Event, 0, services*2)
	for i := 0; i < services; i++ {
		id := fmt.Sprintf("r-%d", i)
		socket := fmt.Sprintf("s-%d", i)
		events = append(events, model.Event{Version: 1, ID: "http-" + id, Type: "http", Time: start.Add(time.Duration(i) * time.Millisecond), RequestID: id, Service: fmt.Sprintf("service-%d", i), Peer: fmt.Sprintf("service-%d", (i+1)%services), SocketID: socket, DurationMS: 40, Status: 200})
		events = append(events, model.Event{Version: 1, ID: "tcp-" + id, Type: "tcp_retransmit", Time: start.Add(time.Duration(i)*time.Millisecond + 10*time.Millisecond), SocketID: socket})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := Build(events, fmt.Sprintf("r-%d", i%services)); !ok {
			b.Fatal("missing request")
		}
	}
}

func BenchmarkReconstruct10Services(b *testing.B)  { benchmarkReconstruct(b, 10) }
func BenchmarkReconstruct50Services(b *testing.B)  { benchmarkReconstruct(b, 50) }
func BenchmarkReconstruct100Services(b *testing.B) { benchmarkReconstruct(b, 100) }

func TestTopology(t *testing.T) {
	e := []model.Event{{Type: "http", Service: "a", Peer: "b", Status: 200}, {Type: "http", Service: "a", Peer: "b", Status: 503}, {Type: "dns", Service: "a", Peer: "b"}}
	edges := Topology(e)
	if len(edges) != 1 || edges[0].Requests != 2 || edges[0].Failures != 1 {
		t.Fatalf("wrong edges: %+v", edges)
	}
}
