package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/raghavtk/skopos/internal/model"
)

func TestReplayAndDeduplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e := model.Event{Version: 1, ID: "one", Type: "dns", Time: time.Now().UTC()}
	if ok, err := s.Add(e); err != nil || !ok {
		t.Fatalf("add: %v %v", ok, err)
	}
	if ok, err := s.Add(e); err != nil || ok {
		t.Fatalf("duplicate: %v %v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := len(s.Events()); got != 1 {
		t.Fatalf("replayed %d events", got)
	}
}
