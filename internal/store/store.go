package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/raghavtk/skopos/internal/model"
)

// Store appends one JSON event per line and replays it at startup. IDs make
// retries idempotent. The file is intentionally a small-lab WAL, not a cluster DB.
type Store struct {
	mu     sync.RWMutex
	file   *os.File
	events []model.Event
	seen   map[string]struct{}
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return nil, err
	}
	s := &Store{file: f, seen: make(map[string]struct{})}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e model.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			f.Close()
			return nil, fmt.Errorf("invalid WAL record: %w", err)
		}
		if _, ok := s.seen[e.ID]; !ok {
			s.seen[e.ID] = struct{}{}
			s.events = append(s.events, e)
		}
	}
	if err := sc.Err(); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, 2); err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Add(e model.Event) (bool, error) {
	n, err := s.AddBatch([]model.Event{e})
	return n == 1, err
}

// AddBatch validates all events first, then persists the new records with one
// write and one fsync. Duplicates within a batch and across retries are ignored.
func (s *Store) AddBatch(batch []model.Event) (int, error) {
	for _, e := range batch {
		if e.Version != 1 || e.ID == "" || e.Type == "" || e.Time.IsZero() {
			return 0, errors.New("event requires version=1, id, type and time")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var buffer bytes.Buffer
	newEvents := make([]model.Event, 0, len(batch))
	inBatch := make(map[string]struct{})
	for _, e := range batch {
		if _, ok := s.seen[e.ID]; ok {
			continue
		}
		if _, ok := inBatch[e.ID]; ok {
			continue
		}
		b, err := json.Marshal(e)
		if err != nil {
			return 0, err
		}
		buffer.Write(b)
		buffer.WriteByte('\n')
		newEvents = append(newEvents, e)
		inBatch[e.ID] = struct{}{}
	}
	if len(newEvents) == 0 {
		return 0, nil
	}
	if _, err := s.file.Write(buffer.Bytes()); err != nil {
		return 0, err
	}
	if err := s.file.Sync(); err != nil {
		return 0, err
	}
	for _, e := range newEvents {
		s.seen[e.ID] = struct{}{}
		s.events = append(s.events, e)
	}
	return len(newEvents), nil
}

func (s *Store) Events() []model.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *Store) Close() error { s.mu.Lock(); defer s.mu.Unlock(); return s.file.Close() }
