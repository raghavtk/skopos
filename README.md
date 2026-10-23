# Skopos

Skopos is an early implementation of an evidence-based request reconstruction system for Kubernetes. It joins observed HTTP transactions to node events by request ID and socket identity, produces a request timeline and diagnosis, shows a service topology, and exports reconstructed spans as OTLP/HTTP JSON.

The first kernel collector captures TCP retransmissions and process exits with C CO-RE eBPF probes. The rest of the event schema supports DNS, packet, policy, route, Service, and pod evidence, but live collectors for those sources are still to be built. The included `abc123` demonstration uses synthetic events to exercise the full query path; it is not evidence of live HTTP or Kubernetes capture.

## Quick start

Requires Go 1.19 or newer. From the repository root:

```sh
go test ./...
go run ./cmd/skopos serve -listen 127.0.0.1:8080
```

In another terminal:

```sh
go run ./cmd/skopos demo
go run ./cmd/skopos request abc123
go run ./cmd/skopos topology
go run ./cmd/skopos otlp abc123
```

Open `http://127.0.0.1:8080` for the request timeline and topology. Events persist in `data/events.jsonl`. `skopos demo` uses stable IDs, so running it again reports duplicates rather than adding another copy.

To send a reconstructed request to an OTLP/HTTP receiver:

```sh
go run ./cmd/skopos export -otlp http://127.0.0.1:4318/v1/traces abc123
```

The `/v1/requests/{id}/otlp` endpoint returns an OTLP JSON `ExportTraceServiceRequest`. The enclosing span is explicitly marked synthetic. Attempt spans describe observed calls, and evidence appears as span events. An existing 32-character hexadecimal trace ID is preserved; otherwise Skopos derives a stable ID from the request ID.

## Linux collector

The agent needs a Linux kernel with BTF and ring-buffer support, Clang, libbpf headers, and privileges to load and attach BPF programs. On Ubuntu or Debian:

```sh
sudo apt-get install clang llvm libbpf-dev make
make bpf
go build -o bin/skopos-agent ./cmd/skopos-agent
sudo bin/skopos-agent -bpf-object bpf/skopos.bpf.o -server http://127.0.0.1:8080
```

The BPF object has **not** been compiled or verifier-tested in this Windows development environment. The Go agent has been cross-compiled for Linux. The collector currently emits node, boot ID, monotonic timestamp, socket cookie, IPv4 tuple, and current kernel-task identity for retransmission events. The current task may differ from the socket owner, so Skopos does not attribute a retransmission to a process from that field. It does not yet resolve socket fields to pod or Service identity. Process-exit events are restricted to thread-group leaders.

`Dockerfile` builds both binaries and the BPF object on Linux. `deploy/dev.yaml` starts one control plane and an agent DaemonSet. It is a development manifest: the control plane uses ephemeral storage, agent delivery is best effort, and the agent is privileged. Load the locally built `skopos:dev` image into every cluster node before applying it.

## Data model and API

Ingest a JSON array of version 1 events with `POST /v1/events`. Each event needs `version`, `id`, `type`, and a UTC `time`. IDs make retries idempotent. Set `SKOPOS_TOKEN` on the server and agent to require a bearer token for ingestion.

Read endpoints:

- `GET /v1/health`
- `GET /v1/events?type=&node=&request_id=&limit=`
- `GET /v1/requests/{id}`
- `GET /v1/requests/{id}/otlp`
- `GET /v1/topology`

The control plane stores newline-delimited JSON in an append-only local file and fsyncs each accepted batch. This is suitable for a single-node lab. ClickHouse, retention, indexing, multi-controller coordination, and agent disk spooling are not implemented.

Correlation joins HTTP records sharing an existing request or trace ID. Non-HTTP evidence joins within a bounded request window when it has the same request ID or socket cookie. Pod, policy, or process events can join by destination identity with lower confidence. IP-only coincidence does not establish request identity. A request's `correlation: exact` describes its ID grouping; each attached evidence item has its own confidence.

Diagnostics are deterministic and cite event IDs. They describe observed conditions; they do not assert that a slow call was caused by retransmission without stronger evidence. The current confidence values are heuristics and have not been calibrated against a labeled dataset.

## Benchmark and validation

Run the portable tests and synthetic 10, 50, and 100-service reconstruction benchmarks:

```sh
go test ./...
go test ./internal/correlate -run '^$' -bench BenchmarkReconstruct -benchmem
```

These benchmarks measure the in-memory correlator only. They do not measure eBPF overhead, request accuracy, dropped events, kernel cost, or diagnostic quality. Those require the Linux cluster and independent ground-truth oracle in the roadmap.

## Remaining milestones

1. Compile and verifier-test the BPF programs on the target Linux kernel; add kernel integration tests and capability detection.
2. Capture real HTTP/1.1 transactions and request headers without application code changes; handle keep-alive and retries.
3. Add OpenSSL plaintext uprobes, DNS and TCP connection state probes, packet drops, route changes, and Kubernetes pod/Service/EndpointSlice watches.
4. Add node disk spooling, clock calibration, batching metrics, retention, and ClickHouse storage.
5. Automate the ten requested failure scenarios in a three-node Kubernetes lab and compare output against an independent oracle.
6. Calibrate correlation confidence, measure overhead and accuracy at 10/50/100 services, and add an evidence-only diagnostic agent if the deterministic baseline is reliable.

Source repository: [github.com/raghavtk/skopos](https://github.com/raghavtk/skopos).
