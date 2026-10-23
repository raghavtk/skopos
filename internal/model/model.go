package model

import "time"

// Event is the versioned interchange record produced by a node collector.
// Time is UTC; MonoNS and BootID preserve ordering within a node when clocks drift.
type Event struct {
	Version    int               `json:"version"`
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Time       time.Time         `json:"time"`
	Node       string            `json:"node,omitempty"`
	BootID     string            `json:"boot_id,omitempty"`
	MonoNS     int64             `json:"mono_ns,omitempty"`
	PID        int               `json:"pid,omitempty"`
	Pod        string            `json:"pod,omitempty"`
	Namespace  string            `json:"namespace,omitempty"`
	Service    string            `json:"service,omitempty"`
	Peer       string            `json:"peer,omitempty"`
	SourceIP   string            `json:"source_ip,omitempty"`
	DestIP     string            `json:"dest_ip,omitempty"`
	SourcePort int               `json:"source_port,omitempty"`
	DestPort   int               `json:"dest_port,omitempty"`
	SocketID   string            `json:"socket_id,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	TraceID    string            `json:"trace_id,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	Status     int               `json:"status,omitempty"`
	DurationMS float64           `json:"duration_ms,omitempty"`
	DNSName    string            `json:"dns_name,omitempty"`
	DNSCode    string            `json:"dns_code,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Evidence struct {
	EventID    string    `json:"event_id"`
	Type       string    `json:"type"`
	Time       time.Time `json:"time"`
	Node       string    `json:"node,omitempty"`
	Summary    string    `json:"summary"`
	Confidence float64   `json:"confidence"`
}

type Attempt struct {
	Service     string    `json:"service"`
	Peer        string    `json:"peer,omitempty"`
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	DurationMS  float64   `json:"duration_ms"`
	Status      int       `json:"status,omitempty"`
	SocketID    string    `json:"socket_id,omitempty"`
	EvidenceIDs []string  `json:"evidence_ids"`
}

type Diagnosis struct {
	Cause       string   `json:"cause"`
	Summary     string   `json:"summary"`
	Confidence  float64  `json:"confidence"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type Request struct {
	ID          string      `json:"id"`
	TraceID     string      `json:"trace_id,omitempty"`
	Start       time.Time   `json:"start"`
	End         time.Time   `json:"end"`
	DurationMS  float64     `json:"duration_ms"`
	Correlation string      `json:"correlation"`
	Confidence  float64     `json:"confidence"`
	Services    []string    `json:"services"`
	Attempts    []Attempt   `json:"attempts"`
	Evidence    []Evidence  `json:"evidence"`
	Diagnoses   []Diagnosis `json:"diagnoses"`
}

type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Requests int    `json:"requests"`
	Failures int    `json:"failures"`
}
