package otlp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/raghavtk/skopos/internal/model"
)

func TestEncodeOTLPJSON(t *testing.T) {
	start := time.Unix(100, 0)
	r := model.Request{ID: "abc123", Start: start, End: start.Add(time.Second), Correlation: "exact", Attempts: []model.Attempt{{Service: "a", Peer: "b", Start: start, End: start.Add(time.Second)}}, Evidence: []model.Evidence{{EventID: "e1", Type: "tcp_retransmit", Time: start, Summary: "retransmission"}}}
	b, err := json.Marshal(Encode(r))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"resourceSpans"`, `"scopeSpans"`, `"traceId"`, `"skopos.evidence.id"`, `"parentSpanId"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s", want)
		}
	}
}
