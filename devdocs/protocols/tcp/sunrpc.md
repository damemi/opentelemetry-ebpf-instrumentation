# OBI SunRPC (ONC RPC) protocol parser

Generic SunRPC over TCP is detected in userspace and exported as traces when `sunrpc` instrumentation is enabled.

## What is captured

Spans are created from the cleartext RPC header of `CALL` messages:

- `rpc.system` = `sunrpc`
- `rpc.service` = program name when known (for example `nfs`, `mount`, `portmapper`), otherwise the numeric program id
- `rpc.method` = procedure number (or a known procedure name when mapped)
- `sunrpc.auth.flavor` = authentication flavor when present (for example `rpcsec_gss`)

Accepted-reply status from the paired response buffer sets span error status when the accept stat is not `SUCCESS`.

## Limitations

- TCP only (no UDP SunRPC).
- RPCSEC_GSS and other security layers protect procedure arguments; only the fixed CALL header fields are visible on the wire.
- No distributed context propagation on SunRPC.
- Metrics export is not implemented in the initial MVP (traces only).
