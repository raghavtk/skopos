package correlate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/raghavtk/skopos/internal/model"
)

// Build groups observed HTTP transactions by an existing request/trace ID.
// Socket events join only inside an HTTP attempt's lifetime. This deliberately
// does not claim causal certainty for unrelated processes sharing an endpoint.
func Build(events []model.Event, id string) (model.Request, bool) {
	var http []model.Event
	for _, e := range events {
		if e.Type == "http" && (e.RequestID == id || e.TraceID == id) {
			http = append(http, e)
		}
	}
	if len(http) == 0 {
		return model.Request{}, false
	}
	sort.Slice(http, func(i, j int) bool { return http[i].Time.Before(http[j].Time) })
	r := model.Request{ID: id, Start: http[0].Time, Correlation: "exact", Confidence: 1, Attempts: []model.Attempt{}, Evidence: []model.Evidence{}, Diagnoses: []model.Diagnosis{}}
	services := map[string]struct{}{}
	evidence := map[string]struct{}{}
	add := func(e model.Event, confidence float64) {
		if _, ok := evidence[e.ID]; ok {
			return
		}
		evidence[e.ID] = struct{}{}
		r.Evidence = append(r.Evidence, model.Evidence{EventID: e.ID, Type: e.Type, Time: e.Time, Node: e.Node, Summary: summary(e), Confidence: confidence})
	}
	for _, h := range http {
		if h.TraceID != "" {
			r.TraceID = h.TraceID
		}
		end := h.Time.Add(time.Duration(h.DurationMS * float64(time.Millisecond)))
		if end.After(r.End) {
			r.End = end
		}
		if h.Service != "" {
			services[h.Service] = struct{}{}
		}
		if h.Peer != "" {
			services[h.Peer] = struct{}{}
		}
		a := model.Attempt{Service: h.Service, Peer: h.Peer, Start: h.Time, End: end, DurationMS: h.DurationMS, Status: h.Status, SocketID: h.SocketID, EvidenceIDs: []string{h.ID}}
		add(h, 1)
		for _, e := range events {
			if e.ID == h.ID || e.Type == "http" {
				continue
			}
			// Bounded causal window. A socket cookie is a stronger join than a
			// coincident IP address; no IP-only match is accepted in v1.
			if e.Time.Before(h.Time.Add(-50*time.Millisecond)) || e.Time.After(end.Add(50*time.Millisecond)) {
				continue
			}
			sameID := e.RequestID == id && id != ""
			sameSocket := h.SocketID != "" && e.SocketID == h.SocketID
			samePeer := (h.Peer != "" && (e.Pod == h.Peer || e.Service == h.Peer))
			if !sameID && !sameSocket && !(samePeer && (e.Type == "pod" || e.Type == "process_exit" || e.Type == "policy")) {
				continue
			}
			confidence := 0.75
			if sameSocket {
				confidence = 0.98
			}
			if sameID {
				confidence = 1
			}
			add(e, confidence)
			a.EvidenceIDs = append(a.EvidenceIDs, e.ID)
		}
		r.Attempts = append(r.Attempts, a)
	}
	for s := range services {
		r.Services = append(r.Services, s)
	}
	sort.Strings(r.Services)
	sort.Slice(r.Evidence, func(i, j int) bool { return r.Evidence[i].Time.Before(r.Evidence[j].Time) })
	r.DurationMS = r.End.Sub(r.Start).Seconds() * 1000
	r.Diagnoses = Diagnose(r)
	return r, true
}

func summary(e model.Event) string {
	switch e.Type {
	case "http":
		return fmt.Sprintf("%s %s %s → %s: HTTP %d in %.1f ms", e.Service, e.Method, e.Path, e.Peer, e.Status, e.DurationMS)
	case "tcp_retransmit":
		return "TCP retransmission on socket " + e.SocketID
	case "tcp_refused":
		return "TCP connection refused by " + e.Peer
	case "packet_drop":
		return "Packet dropped: " + e.Reason
	case "dns":
		return fmt.Sprintf("DNS %s: %s", e.DNSName, e.DNSCode)
	case "pod":
		return fmt.Sprintf("Pod %s: %s", e.Pod, e.Reason)
	case "policy":
		return "Network policy: " + e.Reason
	case "route":
		return "Route: " + e.Reason
	case "process_exit":
		return fmt.Sprintf("Process %d exited: %s", e.PID, e.Reason)
	default:
		return strings.ReplaceAll(e.Type, "_", " ") + ": " + e.Reason
	}
}

// Diagnose uses observed evidence only. A finding is not emitted unless a
// matching event is attached to the request; ambiguous symptoms stay visible.
func Diagnose(r model.Request) []model.Diagnosis {
	type finding struct {
		cause, summary string
		confidence     float64
		ids            []string
	}
	f := map[string]*finding{}
	for _, e := range r.Evidence {
		cause, desc, confidence := "", "", 0.0
		switch e.Type {
		case "tcp_retransmit":
			cause, desc, confidence = "tcp_retransmissions", "TCP retransmissions occurred during the downstream call", 0.9
		case "tcp_refused":
			cause, desc, confidence = "connection_refused", "The destination refused a TCP connection", 0.98
		case "packet_drop":
			cause, desc, confidence = "packet_drop", "A packet was dropped during the request", 0.85
		case "dns":
			if strings.Contains(strings.ToUpper(e.Summary), "NXDOMAIN") || strings.Contains(strings.ToUpper(e.Summary), "SERVFAIL") || strings.Contains(strings.ToUpper(e.Summary), "TIMEOUT") {
				cause, desc, confidence = "dns_failure", "DNS resolution failed", 0.95
			}
		case "pod":
			if strings.Contains(strings.ToLower(e.Summary), "unready") || strings.Contains(strings.ToLower(e.Summary), "rescheduled") || strings.Contains(strings.ToLower(e.Summary), "terminated") {
				cause, desc, confidence = "pod_unhealthy", "A destination pod changed health or placement", 0.85
			}
		case "policy":
			cause, desc, confidence = "policy_denial", "A network policy denied the flow", 0.98
		case "route":
			cause, desc, confidence = "route_problem", "A route change or failure affected the flow", 0.85
		case "process_exit":
			cause, desc, confidence = "process_crash", "A process exited during the request", 0.9
		case "service":
			cause, desc, confidence = "service_misconfiguration", "The Service or its endpoints were misconfigured", 0.9
		}
		if cause == "" {
			continue
		}
		if f[cause] == nil {
			f[cause] = &finding{cause: cause, summary: desc, confidence: confidence}
		}
		f[cause].ids = append(f[cause].ids, e.EventID)
	}
	for _, a := range r.Attempts {
		if a.DurationMS >= 250 && a.Status >= 200 && a.Status < 400 {
			if f["slow_downstream"] == nil {
				f["slow_downstream"] = &finding{cause: "slow_downstream", summary: "A downstream call completed slowly; network and application time are not separable from this observation alone", confidence: 0.55, ids: append([]string(nil), a.EvidenceIDs...)}
			}
		}
	}
	out := make([]model.Diagnosis, 0, len(f))
	for _, x := range f {
		out = append(out, model.Diagnosis{Cause: x.cause, Summary: x.summary, Confidence: x.confidence, EvidenceIDs: x.ids})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out
}

func Topology(events []model.Event) []model.Edge {
	m := map[string]*model.Edge{}
	for _, e := range events {
		if e.Type != "http" || e.Service == "" || e.Peer == "" {
			continue
		}
		key := e.Service + "\x00" + e.Peer
		if m[key] == nil {
			m[key] = &model.Edge{From: e.Service, To: e.Peer}
		}
		m[key].Requests++
		if e.Status >= 400 || e.Status == 0 {
			m[key].Failures++
		}
	}
	out := make([]model.Edge, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From == out[j].From {
			return out[i].To < out[j].To
		}
		return out[i].From < out[j].From
	})
	return out
}
