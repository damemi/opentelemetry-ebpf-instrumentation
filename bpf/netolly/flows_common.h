// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

#pragma once

#include <bpfcore/vmlinux.h>
#include <bpfcore/bpf_helpers.h>

#include <common/connection_info.h>
#include <common/map_sizing.h>
#include <common/pin_internal.h>

#include <netolly/flow.h>

#define DISCARD 1
#define SUBMIT 0

// according to field 61 in https://www.iana.org/assignments/ipfix/ipfix.xhtml
#define INGRESS 0
#define EGRESS 1
#define UNKNOWN 255

// Flags according to RFC 9293 & https://www.iana.org/assignments/ipfix/ipfix.xhtml
#define FIN_FLAG 0x01
#define SYN_FLAG 0x02
#define RST_FLAG 0x04
#define PSH_FLAG 0x08
#define ACK_FLAG 0x10
#define URG_FLAG 0x20
#define ECE_FLAG 0x40
#define CWR_FLAG 0x80
// Custom flags exported
#define SYN_ACK_FLAG 0x100
#define FIN_ACK_FLAG 0x200
#define RST_ACK_FLAG 0x400

// In conn_initiator_key, which sorted ip:port initiated the connection
#define INITIATOR_LOW 1
#define INITIATOR_HIGH 2

// In flow_metrics, who initiated the connection
#define INITIATOR_SRC 1
#define INITIATOR_DST 2

#define INITIATOR_UNKNOWN 0

// Common Ringbuffer as a conduit for ingress/egress flows to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24);
} direct_flows SEC(".maps");

// Key: the flow identifier. Value: the flow metrics for that identifier.
// The userspace will aggregate them into a single flow.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_PERCPU_HASH);
    __type(key, flow_id);
    __type(value, flow_metrics);
} aggregated_flows SEC(".maps");

// Selected NetworkMetrics PIDs (host-side). Kprobes only record sockets for these PIDs.
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, u32);
    __type(value, u8);
    __uint(pinning, OBI_PIN_INTERNAL);
} selected_net_pids SEC(".maps");

// Sorted 5-tuple → owning PID, filled from process-aware kprobes.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, MAX_CONCURRENT_SHARED_REQUESTS);
    __type(key, connection_info_t);
    __type(value, u32);
    __uint(pinning, OBI_PIN_INTERNAL);
} sock_flow_pids SEC(".maps");

// Same metrics as aggregated_flows, keyed by flow_id + PID so shared-netns
// processes are not merged. Only written when sock_flow_pids hits.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_PERCPU_HASH);
    __type(key, pid_flow_id);
    __type(value, flow_metrics);
    __uint(pinning, OBI_PIN_INTERNAL);
} aggregated_flows_pid SEC(".maps");

typedef struct packet_count_t {
    u64 total;
    u64 ignored;
} packet_count;

// Accounts the proportion of packets not reaching the userspace due to an error
// in the allocation/update of direct_flows or aggregated_flows
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __type(key, u32);
    __type(value, packet_count);
    __uint(max_entries, 1);
} flow_packet_stats SEC(".maps");

// Key: the flow identifier. Value: the flow direction.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __type(key, flow_id);
    __type(value, u8);
} flow_directions SEC(".maps");

// To know who initiated each connection, we store the src/dst ip:ports but ordered
// by numeric value of the IP (and port as secondary criteria), so the key is consistent
// for either client and server flows.
typedef struct conn_initiator_key_t {
    struct in6_addr low_ip;
    struct in6_addr high_ip;
    u16 low_ip_port;
    u16 high_ip_port;
} __attribute__((packed)) conn_initiator_key;

// Key: the flow identifier.
// Value: the connection initiator index (INITIATOR_LOW, INITIATOR_HIGH).
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __type(key, conn_initiator_key);
    __type(value, u8);
} conn_initiators SEC(".maps");

const u8 ip4in6[] = {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff};

// Constant definitions, to be overridden by the invoker
volatile const u32 sampling = 0;
volatile const u8 trace_messages = 0;

// Port guessing policy constants
enum { PORT_GUESSING_NONE = 0, PORT_GUESSING_ORDINAL = 1 };
volatile const u8 port_guessing = PORT_GUESSING_NONE;

// we can safely assume that the passed address is IPv6 as long as we encode IPv4
// as IPv6 during the creation of the flow_id.
static inline s32 compare_ipv6(flow_id *fid) {
    for (int i = 0; i < 4; i++) {
        s32 diff = fid->src_ip.in6_u.u6_addr32[i] - fid->dst_ip.in6_u.u6_addr32[i];
        if (diff != 0) {
            return diff;
        }
    }
    return 0;
}

// creates a key that is consistent for both requests and responses, by
// ordering endpoints (ip:port) numerically into a lower and a higher endpoint.
// returns true if the lower address corresponds to the source address
// (false if the lower address corresponds to the destination address)
static inline u8 fill_conn_initiator_key(flow_id *id, conn_initiator_key *key) {
    s32 cmp = compare_ipv6(id);
    if (cmp < 0) {
        __builtin_memcpy(&key->low_ip, &id->src_ip, sizeof(struct in6_addr));
        key->low_ip_port = id->src_port;
        __builtin_memcpy(&key->high_ip, &id->dst_ip, sizeof(struct in6_addr));
        key->high_ip_port = id->dst_port;
        return 1;
    }
    // if the IPs are equal (cmp == 0) we will use the ports as secondary order criteria
    __builtin_memcpy(&key->high_ip, &id->src_ip, sizeof(struct in6_addr));
    __builtin_memcpy(&key->low_ip, &id->dst_ip, sizeof(struct in6_addr));
    if (cmp > 0 || id->src_port > id->dst_port) {
        key->high_ip_port = id->src_port;
        key->low_ip_port = id->dst_port;
        return 0;
    }
    key->low_ip_port = id->src_port;
    key->high_ip_port = id->dst_port;
    return 1;
}

// returns INITIATOR_SRC or INITIATOR_DST, but might return INITIATOR_UNKNOWN
// if the connection initiator couldn't be found. The user-space OBI pipeline
// will handle this last case heuristically
static inline u8 get_connection_initiator(flow_id *id, u16 flags) {
    conn_initiator_key initiator_key;
    // from the initiator_key with sorted ip/ports, know the index of the
    // endpoint that that initiated the connection, which might be the low or the high address
    const u8 low_is_src = fill_conn_initiator_key(id, &initiator_key);
    u8 *initiator = (u8 *)bpf_map_lookup_elem(&conn_initiators, &initiator_key);
    u8 initiator_index = INITIATOR_UNKNOWN;
    if (initiator == NULL) {
        // SYN and ACK is sent from the server to the client
        // The initiator is the destination address
        if ((flags & (SYN_FLAG | ACK_FLAG)) == (SYN_FLAG | ACK_FLAG)) {
            if (low_is_src) {
                initiator_index = INITIATOR_HIGH;
            } else {
                initiator_index = INITIATOR_LOW;
            }
        }
        // SYN is sent from the client to the server.
        // The initiator is the source address
        else if (flags & SYN_FLAG) {
            if (low_is_src) {
                initiator_index = INITIATOR_LOW;
            } else {
                initiator_index = INITIATOR_HIGH;
            }
        }

        if (initiator_index != INITIATOR_UNKNOWN) {
            bpf_map_update_elem(&conn_initiators, &initiator_key, &initiator_index, BPF_NOEXIST);
        }
    } else {
        initiator_index = *initiator;
    }

    // when flow receives FIN or RST, clean flow_directions
    if (flags & FIN_FLAG || flags & RST_FLAG || flags & FIN_ACK_FLAG || flags & RST_ACK_FLAG) {
        bpf_map_delete_elem(&conn_initiators, &initiator_key);
    }

    u8 flow_initiator = INITIATOR_UNKNOWN;
    // at this point, we should know the index of the endpoint that initiated the connection.
    // Then we accordingly set whether the initiator is the source or the destination address.
    // If not, we forward the unknown status and the userspace will take
    // heuristic actions to guess who is
    switch (initiator_index) {
    case INITIATOR_LOW:
        if (low_is_src) {
            flow_initiator = INITIATOR_SRC;
        } else {
            flow_initiator = INITIATOR_DST;
        }
        break;
    case INITIATOR_HIGH:
        if (low_is_src) {
            flow_initiator = INITIATOR_DST;
        } else {
            flow_initiator = INITIATOR_SRC;
        }
        break;
    default:
        break;
    }

    return flow_initiator;
}

static __always_inline u32 lookup_flow_owner_pid(const flow_id *id) {
    connection_info_t conn;
    __builtin_memset(&conn, 0, sizeof(conn));
    __builtin_memcpy(conn.s_addr, id->src_ip.s6_addr, IP_V6_ADDR_LEN);
    __builtin_memcpy(conn.d_addr, id->dst_ip.s6_addr, IP_V6_ADDR_LEN);
    conn.s_port = id->src_port;
    conn.d_port = id->dst_port;
    sort_connection_info(&conn);

    const u32 *pid = bpf_map_lookup_elem(&sock_flow_pids, &conn);
    if (!pid) {
        return 0;
    }
    return *pid;
}

// Accounts the packet in aggregated_flows_pid when a selected PID owns the
// connection. Returns 1 if the packet was consumed by the PID path (do not
// also write aggregated_flows — that would double-count).
static __always_inline int try_account_pid_flow(const flow_id *id,
                                                struct __sk_buff *skb,
                                                u16 flags,
                                                u64 current_time,
                                                packet_count *packet_stats) {
    const u32 pid = lookup_flow_owner_pid(id);
    if (!pid) {
        return 0;
    }

    pid_flow_id pid_id;
    __builtin_memset(&pid_id, 0, sizeof(pid_id));
    pid_id.id = *id;
    pid_id.pid = pid;

    flow_metrics *aggregate_flow =
        (flow_metrics *)bpf_map_lookup_elem(&aggregated_flows_pid, &pid_id);
    if (aggregate_flow) {
        aggregate_flow->packets += 1;
        aggregate_flow->bytes += skb->len;
        aggregate_flow->end_mono_time_ns = current_time;
        if (aggregate_flow->start_mono_time_ns == 0) {
            aggregate_flow->start_mono_time_ns = current_time;
        }
        aggregate_flow->flags |= flags;

        const long ret =
            bpf_map_update_elem(&aggregated_flows_pid, &pid_id, aggregate_flow, BPF_ANY);
        if (trace_messages && ret != 0) {
            bpf_dbg_printk("error updating pid flow, ret=%d. Bytes=%d\n", ret, skb->len);
            if (packet_stats) {
                packet_stats->ignored++;
            }
        }
        return 1;
    }

    flow_metrics new_flow = {
        .packets = 1,
        .bytes = skb->len,
        .start_mono_time_ns = current_time,
        .end_mono_time_ns = current_time,
        .flags = flags,
        .iface_direction = UNKNOWN,
        .initiator = INITIATOR_UNKNOWN,
    };

    u8 *direction = (u8 *)bpf_map_lookup_elem(&flow_directions, id);
    if (direction == NULL) {
        if ((flags & SYN_ACK_FLAG) == SYN_ACK_FLAG) {
            new_flow.iface_direction = INGRESS;
        } else if ((flags & SYN_FLAG) == SYN_FLAG) {
            new_flow.iface_direction = EGRESS;
        }
        if (new_flow.iface_direction != UNKNOWN) {
            bpf_map_update_elem(&flow_directions, id, &new_flow.iface_direction, BPF_NOEXIST);
        } else if (port_guessing == PORT_GUESSING_ORDINAL) {
            new_flow.iface_direction = INGRESS;
            if (id->src_port > id->dst_port) {
                new_flow.iface_direction = EGRESS;
            }
        }
    } else {
        new_flow.iface_direction = *direction;
    }

    new_flow.initiator = get_connection_initiator((flow_id *)id, flags);

    const long ret = bpf_map_update_elem(&aggregated_flows_pid, &pid_id, &new_flow, BPF_ANY);
    if (ret != 0) {
        if (trace_messages) {
            bpf_dbg_printk("error adding pid flow, ret=%d. Bytes=%d\n", ret, skb->len);
        }
        new_flow.errno = -ret;
        flow_record *record =
            (flow_record *)bpf_ringbuf_reserve(&direct_flows, sizeof(flow_record), 0);
        if (!record) {
            if (packet_stats) {
                packet_stats->ignored++;
            }
            return 1;
        }
        record->id = *id;
        record->metrics = new_flow;
        record->pid = pid;
        bpf_ringbuf_submit(record, 0);
    }
    return 1;
}
