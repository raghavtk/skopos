.PHONY: test build bpf linux

test:
	go test ./...

build:
	go build -o bin/skopos ./cmd/skopos

linux:
	GOOS=linux GOARCH=amd64 go build -o bin/skopos ./cmd/skopos
	GOOS=linux GOARCH=amd64 go build -o bin/skopos-agent ./cmd/skopos-agent

bpf:
	clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I/usr/include/$(shell uname -m)-linux-gnu -c bpf/skopos.bpf.c -o bpf/skopos.bpf.o
