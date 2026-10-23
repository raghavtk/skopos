package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raghavtk/skopos/internal/store"
)

func TestIngestAndQuery(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := Server{Store: s, Token: "secret"}.Handler()
	body := `[{"version":1,"id":"e1","type":"http","time":"2026-01-01T00:00:00Z","request_id":"r1","service":"a","peer":"b","duration_ms":20}]`
	req := httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("ingest status %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/requests/r1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"correlation": "exact"`) {
		t.Fatalf("query status %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/topology", nil))
	if !strings.Contains(w.Body.String(), `"from": "a"`) {
		t.Fatalf("missing topology: %s", w.Body.String())
	}
}
