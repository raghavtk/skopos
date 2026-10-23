package otlp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/raghavtk/skopos/internal/model"
)

// Encode returns an OTLP/HTTP JSON ExportTraceServiceRequest. The enclosing
// span is marked synthetic; attempt spans represent observed calls rather than
// a claim about the workload's internal parent/child span relationships.
func Encode(r model.Request) map[string]interface{} {
	traceID := r.TraceID
	if len(traceID) != 32 || !validHex(traceID) {
		traceID = hash(r.ID, 16)
	}
	rootID := hash(r.ID+":root", 8)
	spans := make([]interface{}, 0, len(r.Attempts)+1)
	events := make([]interface{}, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		events = append(events, map[string]interface{}{"timeUnixNano": strconv.FormatInt(e.Time.UnixNano(), 10), "name": "skopos.evidence", "attributes": []interface{}{attr("skopos.evidence.id", e.EventID), attr("skopos.evidence.type", e.Type), attr("skopos.evidence.summary", e.Summary)}})
	}
	spans = append(spans, map[string]interface{}{
		"traceId": traceID, "spanId": rootID, "name": "skopos reconstructed request",
		"kind": 1, "startTimeUnixNano": strconv.FormatInt(r.Start.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(r.End.UnixNano(), 10),
		"attributes": []interface{}{attr("skopos.request.id", r.ID), attr("skopos.correlation.method", r.Correlation), attr("skopos.synthetic", "true")}, "events": events,
	})
	for i, a := range r.Attempts {
		attributes := []interface{}{attr("service.name", a.Service), attr("server.address", a.Peer), attr("skopos.socket.id", a.SocketID)}
		spans = append(spans, map[string]interface{}{
			"traceId": traceID, "spanId": hash(fmt.Sprintf("%s:attempt:%d", r.ID, i), 8), "parentSpanId": rootID,
			"name": a.Service + " → " + a.Peer, "kind": 3, "startTimeUnixNano": strconv.FormatInt(a.Start.UnixNano(), 10), "endTimeUnixNano": strconv.FormatInt(a.End.UnixNano(), 10), "attributes": attributes,
		})
	}
	return map[string]interface{}{"resourceSpans": []interface{}{map[string]interface{}{
		"resource":   map[string]interface{}{"attributes": []interface{}{attr("service.name", "skopos")}},
		"scopeSpans": []interface{}{map[string]interface{}{"scope": map[string]interface{}{"name": "skopos/reconstructor", "version": "0.1.0"}, "spans": spans}},
	}}}
}

func attr(k, v string) map[string]interface{} {
	return map[string]interface{}{"key": k, "value": map[string]string{"stringValue": v}}
}
func hash(s string, n int) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:n]) }
func validHex(s string) bool      { _, err := hex.DecodeString(s); return err == nil }
