# OBI SunRPC (ONC RPC) protocol parser

Generic SunRPC over TCP is detected in userspace and exported as traces when `sunrpc` instrumentation is enabled.

## What is captured

Spans are created from the cleartext RPC header of `CALL` messages using [ONC RPC semantic conventions](https://opentelemetry.io/docs/specs/semconv/registry/attributes/onc-rpc/):

- `rpc.system` = `onc_rpc` (via `semconv.RPCSystemOncRPC`)
- `onc_rpc.program.name` — program name when known (for example `nfs`, `mount`, `portmapper`)
- `onc_rpc.procedure.number` — procedure number from the CALL header
- `onc_rpc.procedure.name` — set when a procedure name mapping exists
- `onc_rpc.version` — program version from the CALL header

Accepted-reply status from the paired response buffer sets span error status when the accept stat is not `SUCCESS`.

## Limitations

- TCP only (no UDP SunRPC).
- RPCSEC_GSS and other security layers protect procedure arguments; only the fixed CALL header fields are visible on the wire.
- No distributed context propagation on SunRPC.
- Metrics export is not implemented in the initial MVP (traces only).
