FROM golang:1.24-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends clang llvm libbpf-dev make && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make bpf && CGO_ENABLED=0 go build -o /out/skopos ./cmd/skopos && CGO_ENABLED=0 go build -o /out/skopos-agent ./cmd/skopos-agent

FROM debian:bookworm-slim
COPY --from=build /out/skopos /usr/local/bin/skopos
COPY --from=build /out/skopos-agent /usr/local/bin/skopos-agent
COPY --from=build /src/bpf/skopos.bpf.o /opt/skopos/skopos.bpf.o
USER 0
ENTRYPOINT ["skopos"]
