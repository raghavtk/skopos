// SPDX-License-Identifier: GPL-2.0
// Build on Linux: clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I/usr/include/$(uname -m)-linux-gnu -c bpf/skopos.bpf.c -o bpf/skopos.bpf.o
#include <linux/types.h>
#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include <bpf/bpf_tracing.h>

// Minimal CO-RE types: only fields accessed below are described. clang emits
// BTF relocations against the target kernel's full struct sock definition.
struct sock_common {
	__be32 skc_daddr;
	__be32 skc_rcv_saddr;
	__be16 skc_dport;
	__u16 skc_num;
} __attribute__((preserve_access_index));
struct sock { struct sock_common __sk_common; } __attribute__((preserve_access_index));

struct event {
	__u64 mono_ns;
	__u64 socket_cookie;
	__u32 pid;
	__u32 type;
	__u32 saddr;
	__u32 daddr;
	__u16 sport;
	__u16 dport;
	char comm[16];
};

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 23);
} events SEC(".maps");

SEC("kprobe/tcp_retransmit_skb")
int trace_retransmit(struct pt_regs *ctx) {
	struct sock *sk = (struct sock *)PT_REGS_PARM1(ctx);
	struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) return 0;
	__builtin_memset(e, 0, sizeof(*e));
	e->mono_ns = bpf_ktime_get_ns();
	e->pid = (__u32)(bpf_get_current_pid_tgid() >> 32);
	e->type = 1;
	e->socket_cookie = bpf_get_socket_cookie(sk);
	e->saddr = BPF_CORE_READ(sk, __sk_common.skc_rcv_saddr);
	e->daddr = BPF_CORE_READ(sk, __sk_common.skc_daddr);
	e->sport = BPF_CORE_READ(sk, __sk_common.skc_num);
	e->dport = __builtin_bswap16(BPF_CORE_READ(sk, __sk_common.skc_dport));
	bpf_get_current_comm(&e->comm, sizeof(e->comm));
	bpf_ringbuf_submit(e, 0);
	return 0;
}

SEC("tracepoint/sched/sched_process_exit")
int trace_exit(void *ctx) {
	__u64 pid_tgid = bpf_get_current_pid_tgid();
	if ((__u32)pid_tgid != (__u32)(pid_tgid >> 32)) return 0;
	struct event *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
	if (!e) return 0;
	__builtin_memset(e, 0, sizeof(*e));
	e->mono_ns = bpf_ktime_get_ns();
	e->pid = (__u32)(pid_tgid >> 32);
	e->type = 2;
	bpf_get_current_comm(&e->comm, sizeof(e->comm));
	bpf_ringbuf_submit(e, 0);
	return 0;
}

char LICENSE[] SEC("license") = "GPL";
