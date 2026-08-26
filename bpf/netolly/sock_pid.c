// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build obi_bpf_ignore

#include <bpfcore/vmlinux.h>
#include <bpfcore/bpf_helpers.h>
#include <bpfcore/bpf_tracing.h>

#include <common/sockaddr.h>

#include <netolly/flows_common.h>
#include <pid/pid_helpers.h>

static __always_inline void store_sock_flow_pid(struct sock *sk) {
    const u32 pid = pid_from_pid_tgid(bpf_get_current_pid_tgid());
    if (!pid) {
        return;
    }
    if (!bpf_map_lookup_elem(&selected_net_pids, &pid)) {
        return;
    }

    connection_info_t conn;
    if (!parse_sock_info(sk, &conn)) {
        return;
    }
    sort_connection_info(&conn);
    bpf_map_update_elem(&sock_flow_pids, &conn, &pid, BPF_ANY);
}

static __always_inline void delete_sock_flow_pid(struct sock *sk) {
    connection_info_t conn;
    if (!parse_sock_info(sk, &conn)) {
        return;
    }
    sort_connection_info(&conn);
    bpf_map_delete_elem(&sock_flow_pids, &conn);
}

SEC("kprobe/tcp_connect")
int BPF_KPROBE(obi_net_kprobe_tcp_connect, struct sock *sk) {
    (void)ctx;
    store_sock_flow_pid(sk);
    return 0;
}

SEC("kprobe/tcp_sendmsg")
int BPF_KPROBE(obi_net_kprobe_tcp_sendmsg, struct sock *sk) {
    (void)ctx;
    store_sock_flow_pid(sk);
    return 0;
}

SEC("kprobe/udp_sendmsg")
int BPF_KPROBE(obi_net_kprobe_udp_sendmsg, struct sock *sk) {
    (void)ctx;
    store_sock_flow_pid(sk);
    return 0;
}

SEC("kretprobe/inet_csk_accept")
int BPF_KRETPROBE(obi_net_kretprobe_inet_csk_accept, struct sock *newsk) {
    (void)ctx;
    if (newsk) {
        store_sock_flow_pid(newsk);
    }
    return 0;
}

SEC("kprobe/tcp_close")
int BPF_KPROBE(obi_net_kprobe_tcp_close, struct sock *sk) {
    (void)ctx;
    delete_sock_flow_pid(sk);
    return 0;
}
