# OBI SunRPC (ONC RPC) protocol parser

Generic SunRPC over TCP is detected in userspace and exported when `sunrpc` instrumentation is enabled.

## Protocol Overview

SunRPC (ONC RPC) uses TCP record marking followed by XDR-encoded CALL and REPLY messages. OBI parses the cleartext CALL header to build spans; RPCSEC_GSS and other security layers leave program, version, and procedure visible while protecting arguments.

See [RFC 5531](https://datatracker.ietf.org/doc/html/rfc5531) for the wire format.

## Traces

Spans use [ONC RPC semantic conventions](https://opentelemetry.io/docs/specs/semconv/registry/attributes/onc-rpc/):

| Attribute | Description |
|:----------|:------------|
| `rpc.system` | `onc_rpc` (`semconv.RPCSystemOncRPC`) |
| `onc_rpc.program.name` | Program name when known (for example `nfs`, `mount`, `portmapper`) |
| `onc_rpc.procedure.number` | Procedure number from the CALL header |
| `onc_rpc.procedure.name` | Procedure name when a mapping exists |
| `onc_rpc.version` | Program version from the CALL header |
| `rpc.auth.flavor` | Authentication flavor when present (extension until semconv adds it) |
| `rpc.response.status_code` | Non-zero when the paired REPLY accept stat is not SUCCESS |

Client spans use `SpanKindClient`; server spans use `SpanKindServer`. Trace names follow `{program}/{procedure}` (for example `portmapper/0`).

Enable traces with `instrumentations: [sunrpc]` under `otel_traces` (enabled by default in the stock config).

## Metrics

SunRPC spans emit standard RPC duration histograms:

| OTEL metric | Prometheus name | Span kinds |
|:------------|:------------------|:-----------|
| `rpc.client.duration` | `rpc_client_duration_seconds` | `SunRPCClient` |
| `rpc.server.duration` | `rpc_server_duration_seconds` | `SunRPCServer` |

Metric attributes include `rpc.system=onc_rpc`, `onc_rpc.program.name`, `onc_rpc.procedure.number`, and `onc_rpc.version` when available.

Enable metrics with `instrumentations: [sunrpc]` under `otel_metrics` or `prometheus` (included when `instrumentations: ["*"]`).

## Limitations

- TCP only (no UDP SunRPC).
- Userspace heuristic detection only (no kernel `protocol_type` classifier yet).
- RPCSEC_GSS hides procedure arguments; only header fields are visible.
- No distributed context propagation on SunRPC.
- Procedure names are not mapped yet (procedure number only unless extended).

## Integration tests

`TestSuite_GoSunRPC` in `internal/test/integration/` runs a minimal portmapper NULL RPC roundtrip via `components/gosunrpc`.
