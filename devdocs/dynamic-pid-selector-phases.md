# Phased rollout for signal-split `DynamicPIDSelector`

This document plans a staged implementation of per-signal dynamic runtime
selection for OBI.

The end goal is one runtime selector object that can target signals
independently:

```go
selector.Traces().AddPIDs(1234)
selector.AppMetrics().AddPIDs(1234)
selector.NetworkMetrics().AddPIDs(1234)
selector.StatsMetrics().AddPIDs(1234)
```

while preserving a compatibility path for existing callers:

```go
selector.AddPIDs(1234)
```

The key design constraint is that different OBI pipelines consume different
runtime identities:

- AppO11y discovery/instrumentation is process-oriented and naturally keyed by
  PID.
- NetO11y and StatsO11y exporters operate on flow/stat records and must filter
  through network identity.

This plan intentionally delivers those pieces through separate, complete PRs so
each step is reviewable and leaves the tree in a coherent state.

## Goals

- Introduce signal-specific dynamic selection without changing the basic "hold
  one selector object and mutate it at runtime" model.
- Keep traces and app metrics sharing discovery/instrumentation lifecycle until
  export-time behavior needs to diverge.
- Keep network and stats independent from AppO11y and from each other.
- Preserve root `AddPIDs(...)` support throughout the rollout.

## Non-goals

- Replace `DynamicPIDSelector` with a workload-level selector such as a
  deployment or label selector.
- Redesign static discovery selectors.
- Rework exporter configuration or metric definitions beyond what is necessary
  for dynamic signal gating.

## End-state model

The intended final model is:

- one root selector storing `PID -> signalMask`
- one internal app-union lifecycle view for discovery/instrumentation
- four user-facing subviews:
  - `Traces()`
  - `AppMetrics()`
  - `NetworkMetrics()`
  - `StatsMetrics()`

Semantics:

- adding a PID twice to the same view is a no-op
- traces + app metrics share one discovery/instrumentation lifecycle
- removing one app signal does not tear down app lifecycle state if the other
  app signal still owns that PID
- network and stats are independently selectable
- root `AddPIDs(...)` remains supported and maps to all supported signals

## Alternatives considered

This section records other designs evaluated for per-signal dynamic selection.
The phased rollout uses **one selector**, **`map[PID]signalMask`**, and
**subviews** (`Traces()`, `AppMetrics()`, …). The alternatives below mostly
differ in API shape or where export policy lives; several are equivalent
internally to a per-PID bitmask.

### Chosen approach (summary)

- **Storage:** `map[app.PID]dynamicPIDSignal` (bitmask per PID).
- **API:** embedded views with per-view `mask` + per-view notifiers; root view
  uses `allSignalMask` for legacy `AddPIDs`.
- **App discovery:** internal `appSignalsView` (`traces | appMetrics`) for
  union lifecycle and notifications.
- **App export split:** Phase 5 gates (`IgnoreTraces` / `IgnoreMetrics`) driven
  by `Traces()` / `AppMetrics()` subviews, not only `svc.ExportModes`.
- **Net/stats:** subviews + `DynamicAppIPs` (PID → pod IP) + `filter.ByDynamicPID`.

### 1. Options on root `AddPIDs` (no subviews)

```go
selector.AddPIDs(42, selection.ForTraces)
// or
selector.AddPIDs(42, selection.Signals{Traces: true, Metrics: false})
```

| Pros | Cons |
|------|------|
| Single object; no `Traces()` methods | Harder to pass a narrow `PIDSelector` into pipelines |
| Same underlying bitmask as subviews | Per-signal notifications still need view-specific channels or tagged batches |

**Verdict:** API preference only. Subviews read clearly in wiring
(`ctxInfo.DynamicPIDSelector.NetworkMetrics()`). Could be offered later as sugar
on top of the same implementation.

### 2. Separate selector instance per signal

```go
type Bundle struct {
    Traces, AppMetrics, Network, Stats *DynamicPIDSet
}
```

Each signal owns a flat `map[PID]struct{}` and its own notifier.

| Pros | Cons |
|------|------|
| No bitmasks; obvious per-signal membership | Conflicts with “one easy reference” unless wrapped in a facade |
| | Four sets to update for root `AddPIDs`, inheritance, and “remove all” |
| | App-union = manual dual-subscribe or a fifth wrapper object |

**Verdict:** Rejected for the library API goal. A thin facade often reintroduces
shared bitmask logic anyway.

### 3. Explicit bool struct or inverted index per signal

Replace `dynamicPIDSignal` with:

```go
type pidEntry struct { Traces, AppMetrics, Network, Stats bool }
// or
sets map[Signal]map[app.PID]struct{}
```

| Pros | Cons |
|------|------|
| More readable in prose | Equivalent behavior to a 4-bit mask |
| Direct per-signal map lookup (inverted index) | Inverted index duplicates PIDs; union/root queries need extra bookkeeping |

**Verdict:** Not adopted; bitmask keeps transition/notification logic compact
(`contains`, OR on add, AND-NOT on remove).

### 4. Pipeline-only split (no selector extension)

Keep a flat dynamic PID list; enforce traces vs metrics only in pipelines via
attribute filters, separate allowlists, or config—not runtime subviews.

| Pros | Cons |
|------|------|
| Smallest change to the selector type | No single runtime API for “traces yes, metrics no” on one object |
| | Net/stats still need PID→IP (or similar) somewhere else |
| | Duplicated “who is selected?” across components |

**Verdict:** Rejected for the stated goal. Attribute/IP filters alone do not
replace per-signal runtime membership and notifications.

### 5. Reuse `services.ExportModes` (static AppO11y model)

**Static AppO11y today:** YAML `exports: [traces]` / `[metrics]` on
`services.Selector`; `typer.makeServiceAttrs` copies `ExportModes` onto
`svc.Attrs`; exporters call `CanExportTraces()` / `CanExportMetrics()`.

**Dynamic path before signal-split:** `dynamicPIDCriteriaAdapter.GetExportModes()`
returns `ExportModeUnset` (allow all signals)—dynamic selection was **membership
only**, not export policy.

**Option 6 variants:**

| Variant | Idea | Gap |
|---------|------|-----|
| **A. Set modes at discovery** | Derive `ExportModes` from selector mask in adapter/typer | Runtime toggles after match do not update spans (`Span.Service` is copied `svc.Attrs`) |
| **B. Live mutate `FileInfo` attrs** | On selector change, update instrumented service `ExportModes` | Buffered spans keep stale attrs; process-event metrics still need synthetic create/delete when metrics toggles |
| **C. Hybrid** | Initial `ExportModes` at match + live selector or `Ignore*` at export | Duplicates policy unless unified |

Mapping from dynamic mask to `ExportModes` is straightforward (e.g. traces bit →
`AllowTraces()` only). **Network and stats are not representable** in
`ExportModes` (only traces, metrics, logs)—net/stats still need selector subviews
or another mechanism.

A future consolidation could expose `exportModesForPID(pid)` used by typer
(initial) and exporters (live), with subviews as mutators only:

```go
modes := span.Service.ExportModes
if dynSel != nil {
    modes = dynSel.ExportModesForPID(dynamicSignalPID(span))
}
```

That unifies export checks on one type but still requires a live selector lookup
(equivalent to today’s gates).

| Pros | Cons |
|------|------|
| Aligns app export with static `exports` | Does not replace PID registry, app-union, or net/stats subviews |
| Exporters already understand `ExportModes` | Competes with static `exports` on merged criteria (typer: last non-unset wins) |
| | Process-event lifecycle for metrics-only toggles still needs gate-like glue |

**Verdict:** Good complement for **app trace/metric export shape**, not a full
replacement for mask + subviews + notifications. Phase 5 intentionally uses
`IgnoreTraces` / `IgnoreMetrics` for **runtime** toggles while leaving
`ExportModeUnset` on the dynamic adapter so static `exports` on the same process
are not overridden accidentally.

### 6. Registration handles / tokens

```go
h := selector.Register(42, selection.Traces)
defer h.Unregister()
```

| Pros | Cons |
|------|------|
| Clear scoped add/remove for callers | Heavy for daemons/UI that hold one selector and flip signals |
| | Still needs shared index and per-signal notifications underneath |

**Verdict:** Not pursued for the agent/library use case.

### 7. Workload-level selection (deployment / pod / labels)

Resolve workloads to PIDs (or IPs) at runtime instead of exposing host PIDs
directly.

| Pros | Cons |
|------|------|
| Matches operator mental model | Different product surface; still resolves to PID/IP sets internally |
| | Listed under **Non-goals** for this rollout |

**Verdict:** Orthogonal future direction; does not replace per-signal membership
storage.

### Comparison

| Requirement | Mask + subviews | ExportModes-only dynamic | Separate selectors |
|-------------|-----------------|--------------------------|-------------------|
| One library handle | Yes | Yes (with facade) | Needs bundle |
| Per-signal `PIDSelector` for pipelines | `NetworkMetrics()` etc. | Options API or bundle | Four objects |
| Per-signal notifications | View notifiers + transition logic | Same problem | Four notifiers |
| App discovery = traces ∪ metrics | `appSignalsView` | Manual union | Dual subscribe |
| Legacy `AddPIDs` = all signals | `rootView` / `allSignalMask` | Call all four | Facade helper |
| Net/stats signals | Dedicated bits + IP filter | Not in `ExportModes` | Dedicated sets |
| Runtime trace/metric toggle | Subviews + Phase 5 gates | Live modes or gates | Same |

### Why this rollout uses mask + subviews

1. **One object** with **narrow interfaces** (`PIDSelector` vs
   `MutablePIDSelector` vs `MultiSignalPIDSelector`) for `pkg/selection` and
   pipeline wiring without import cycles.
2. **Per-signal notify edges** without spamming unrelated consumers (net/stats
   do not wake on trace-only changes).
3. **App-union** as an explicit mask (`appSignalMask`), not ad hoc dual
   subscriptions.
4. **Export split deferred to Phase 5** so selector state (Phase 3) and
   discovery ownership (Phase 4) land before `Ignore*` gates; static
   `ExportModes` remains the config-time policy, dynamic subviews the runtime
   policy.

## Rollout principles

Each PR should:

- tell one clear story
- avoid mixing selector state-model changes with exporter behavior changes unless
  they are inseparable
- keep compatibility behavior explicit and documented
- add tests closest to the behavior introduced by that PR

## Phase 1: Move selector ownership into shared runtime context

### Purpose

Make the dynamic selector a shared runtime concern instead of an AppO11y-only
detail.

### Scope

- move from `ctxInfo.AppO11y.DynamicPIDSelector` to top-level
  `ctxInfo.DynamicPIDSelector`
- update `instrumenter`, `internal/appolly`, and related tests
- introduce a minimal shared selector contract if downstream packages need one

### Behavior after this phase

- no per-signal behavior yet
- AppO11y dynamic PID behavior stays the same
- the selector simply has a stable shared home for later phases

### Detailed implementation

The first phase is intentionally narrow: only move ownership and replace
`any`-typed plumbing with a small shared contract.

Shared context moves from:

```go
type AppO11y struct {
    ReportRoutes bool
    DynamicPIDSelector any
}
```

to:

```go
package selection

type PIDSelector interface {
    GetPIDs() ([]app.PID, bool)
    AddedPIDsNotify() <-chan []app.PID
    RemovedNotify() <-chan []app.PID
}
```

and:

```go
type ContextInfo struct {
    // ...
    DynamicPIDSelector selection.PIDSelector
}

type AppO11y struct {
    ReportRoutes bool
}
```

`instrumenter.WithDynamicPIDSelector(...)` stops writing into
`ctxInfo.AppO11y.DynamicPIDSelector` and instead writes to the top-level field:

```go
func WithDynamicPIDSelector(sel *discover.DynamicPIDSelector) Option {
    return func(info *global.ContextInfo) {
        info.DynamicPIDSelector = sel
    }
}
```

`pkg/internal/appolly/appolly.go` and `pkg/instrumenter/instrumenter.go` then
read from that shared field:

```go
sel, _ := ctxInfo.DynamicPIDSelector.(*discover.DynamicPIDSelector)
```

This phase should not introduce subviews, signal masks, or exporter gating. Its
only pipeline change is that startup logic and AppO11y construction read the
selector from one shared runtime location.

### Why this phase exists

Without this move, later net/stats work would keep reaching through app-only
context or depending on `any`-typed plumbing.

## Phase 2: Extend dynamic filtering to network and stats with current selector semantics

### Purpose

Deliver immediate value by making dynamic selection affect exported net/stats
metrics, while keeping the selector model flat.

### Scope

- add shared PID-to-IP runtime filtering helpers
- add a reusable dynamic filter node for metrics pipelines
- wire NetO11y and StatsO11y through that filter using the existing selector
  semantics

### Behavior after this phase

- dynamically selected applications influence:
  - AppO11y discovery/instrumentation
  - NetO11y export filtering
  - StatsO11y export filtering
- all signals still effectively share one dynamic PID set

### Detailed implementation

This phase should keep the selector contract flat and reuse the same dynamic PID
set everywhere.

The shared runtime helper is a PID-to-IP tracker:

```go
type DynamicAppIPs struct {
    selector PIDSelector
    store    *kube.Store

    mu      sync.RWMutex
    allowed map[string]struct{}
    pidToIPs map[app.PID][]string
}
```

The responsibilities are:

- preload already-selected PIDs from `selector.GetPIDs()`
- watch `AddedPIDsNotify()` / `RemovedNotify()`
- resolve selected PIDs to pod/container IPs via the kube store
- answer `Allows(attrs *pipe.CommonAttrs) bool`

The reusable filter node sits in shared pipeline code:

```go
func ByDynamicPID[T any](
    ctx context.Context,
    selector selection.PIDSelector,
    k8sInformer *kube.MetadataProvider,
    attrs func(T) *pipe.CommonAttrs,
    input, output *msg.Queue[[]T],
) swarm.InstanceFunc
```

NetO11y and StatsO11y then insert one extra stage before their existing
attribute filter:

```go
dynamicFilteredFlows := msgh.QueueFromConfig[[]*ebpf.Record](cfg, "dynamicFilteredFlows")
swi.Add(filter.ByDynamicPID(ctx, ctxInfo.DynamicPIDSelector, ctxInfo.K8sInformer,
    recordAttrs, decoratedFlows, dynamicFilteredFlows))

swi.Add(filter.ByAttribute(..., dynamicFilteredFlows, filteredFlows))
```

and:

```go
dynamicFilteredStats := msgh.QueueFromConfig[[]*ebpf.Stat](cfg, "dynamicFilteredStats")
swi.Add(filter.ByDynamicPID(ctx, ctxInfo.DynamicPIDSelector, ctxInfo.K8sInformer,
    statAttrs, decoratedStats, dynamicFilteredStats))

swi.Add(filter.ByAttribute(..., dynamicFilteredStats, filteredStats))
```

The important constraint for this phase is that no signal split exists yet. If a
PID is in the selector, it affects app discovery and both metrics pipelines.

### Why this phase exists

It ships the original "dynamic metrics selector" behavior independently from the
harder mutual-exclusivity work.

## Phase 3: Refactor `DynamicPIDSelector` into signal-aware views

### Purpose

Change the selector's internal state model without yet changing every pipeline's
behavior.

### Scope

- replace flat PID storage with `map[PID]signalMask`
- add a minimal signal enum and root-level mask operations
- add user-facing subviews:
  - `Traces()`
  - `AppMetrics()`
  - `NetworkMetrics()`
  - `StatsMetrics()`
- preserve root `AddPIDs(...)` compatibility
- add internal app-union notification behavior

### Behavior after this phase

- the selector can represent independent ownership per signal
- callers can start using subviews
- existing root behavior is still available
- discovery/export code may still behave mostly like the old model until later
  phases consume the new views

### Detailed implementation

This phase changes only selector state and its API surface.

Introduce a signal enum:

```go
type dynamicPIDSignal uint8

const (
    signalTraces dynamicPIDSignal = 1 << iota
    signalAppMetrics
    signalNetworkMetrics
    signalStatsMetrics
)

const (
    appSignalMask dynamicPIDSignal = signalTraces | signalAppMetrics
    allSignalMask dynamicPIDSignal = appSignalMask | signalNetworkMetrics | signalStatsMetrics
)
```

Replace flat PID storage:

```go
type DynamicPIDSelector struct {
    mu   sync.RWMutex
    pids []uint32
}
```

with signal-aware state and views:

```go
type DynamicPIDSelector struct {
    mu    sync.RWMutex
    byPID map[app.PID]dynamicPIDSignal

    rootView           dynamicPIDSignalView
    tracesView         dynamicPIDSignalView
    appMetricsView     dynamicPIDSignalView
    networkMetricsView dynamicPIDSignalView
    statsMetricsView   dynamicPIDSignalView
    appSignalsView     dynamicPIDSignalView
}

type dynamicPIDSignalView struct {
    parent   *DynamicPIDSelector
    mask     dynamicPIDSignal
    notifier *dynamicPIDNotifier
}
```

The public selector contract expands to support subviews:

```go
type PIDSelector interface {
    GetPIDs() ([]app.PID, bool)
    IncludesPID(app.PID) bool
    AddedPIDsNotify() <-chan []app.PID
    RemovedNotify() <-chan []app.PID
}

type MutablePIDSelector interface {
    PIDSelector
    AddPIDs(...uint32)
    RemovePIDs(...uint32)
}

type MultiSignalPIDSelector interface {
    MutablePIDSelector
    Traces() MutablePIDSelector
    AppMetrics() MutablePIDSelector
    NetworkMetrics() MutablePIDSelector
    StatsMetrics() MutablePIDSelector
}
```

Place these interfaces in `pkg/selection` (not `pkg/appolly/discover`) so net/stats
and `DynamicAppIPs` can depend on a narrow contract without an import cycle.
`*discover.DynamicPIDSelector` and `*dynamicPIDSignalView` implement them in
`discover`.

#### Relationship between the three interfaces

The three types are one **capability ladder**, not three unrelated APIs. Each
level embeds the previous:

```text
PIDSelector              read membership + change notifications
    ↑ embedded by
MutablePIDSelector       add/remove PIDs for one signal view
    ↑ embedded by
MultiSignalPIDSelector   root mutation + accessors for all signal subviews
```

**`PIDSelector` (read-only contract)**

- `GetPIDs()` / `IncludesPID()` — query membership for a view’s mask
- `AddedPIDsNotify()` / `RemovedNotify()` — subscribe to **that view’s** edges
  (a PID is notified only when it newly enters or leaves the view; see
  per-view membership below)

Consumers that must **listen or filter** but must **not** change runtime
selection take this type. Examples in later phases:

- `selection.NewDynamicAppIPs(selector, store)` — net/stats IP tracking
- `filter.ByDynamicPID(..., selector, ...)` — pipeline filter node
- `DynamicMatcher` matching via `IncludesPID` on the app-union view
- Phase 5 `DynamicSignalProcessEventGate` uses `selector.AppMetrics()` as a
  `PIDSelector` for metrics lifecycle

**`MutablePIDSelector` (single-signal controller)**

- Everything in `PIDSelector`, plus `AddPIDs` / `RemovePIDs` for **one** mask
- Return type of `Traces()`, `AppMetrics()`, `NetworkMetrics()`, `StatsMetrics()`
  (and the internal `appSignals()` view)

Callers that mutate **one** signal pass a subview handle. The type deliberately
does **not** expose `Traces()` / `NetworkMetrics()` on the same value, so pipeline
code cannot accidentally add PIDs to the wrong signal through a shared root
reference.

Each `dynamicPIDSignalView` implements `MutablePIDSelector` by delegating to
`parent.addSignals(v.mask, ...)` / `removeSignals(v.mask, ...)`.

**`MultiSignalPIDSelector` (library entry point)**

- Everything in `MutablePIDSelector`, plus subview accessors
- Type of `ctxInfo.DynamicPIDSelector` and `instrumenter.WithDynamicPIDSelector`
- Implemented by `*DynamicPIDSelector`; root `AddPIDs` / `RemovePIDs` delegate to
  `rootView` (`allSignalMask`) for backward compatibility

Only code that **owns** the selector needs this interface: instrumenter setup,
Phase 5 span/process gates that need both `Traces()` and `AppMetrics()`, and the
caller that holds one reference and toggles multiple signals.

#### Why three interfaces instead of one

| If everything were `MultiSignalPIDSelector` | With the split |
|---------------------------------------------|----------------|
| `ByDynamicPID` could call `AddPIDs` on the root | Filter nodes take `PIDSelector` only |
| A net pipeline handed the root could touch app signals | `NetworkMetrics()` returns `MutablePIDSelector`, not `MultiSignalPIDSelector` |
| No compile-time distinction between observer and mutator | Matches Phase 1 goal: replace `any` with a checked contract at the right width |

Phase 1 may introduce only `PIDSelector` on `ContextInfo`; Phase 3 expands to
the full ladder when subviews appear. The concrete type is still
`*discover.DynamicPIDSelector` at the call site; interfaces describe **what each
package is allowed to do**.

#### Interfaces vs bitmask storage

The bitmask (`map[PID]dynamicPIDSignal`) is **storage**. The interfaces are **who
may read or write** that storage and through which view. Subviews are the user-
visible handles; interfaces are how other packages refer to those handles without
importing `discover`.

#### Per-view membership and app-union (selector internals)

Membership is **not** mutually exclusive across signals: one PID can have traces,
app metrics, network, and stats bits set independently (`storedMask` is a
bitfield).

- **Per-view membership:** view `V` includes PID `p` when `(storedMask[p] & V.mask) != 0`.
- **Notifications:** view `V` fires **added** when `p` was outside `V` before the
  operation and inside after; **removed** when the opposite. Adding a second
  signal for the same PID does not re-notify views that already contained it.
- **App-union (`appSignalsView`, mask = traces \| app metrics):** internal view
  for discovery lifecycle. A PID is in the union if **either** app bit is set.
  Union **added** when the PID first gains traces or app metrics; union **removed**
  only when **both** app bits are cleared. Net/stats-only selection does not enter
  the app union.

Phase 3 does **not** yet split app export (traces vs metrics at exporters); it only
makes that split **representable** and **notifiable**. Export gating is Phase 5.

Root compatibility stays explicit:

```go
func (d *DynamicPIDSelector) AddPIDs(pids ...uint32) {
    d.rootView.AddPIDs(pids...)
}
```

while views delegate to masked operations:

```go
func (v *dynamicPIDSignalView) AddPIDs(pids ...uint32) {
    v.parent.addSignals(v.mask, pids...)
}
```

No pipeline should yet depend on app-union semantics beyond selector internals.
This phase is complete when selector tests prove:

- root union behavior
- per-view membership
- app-union notifications
- no-op duplicate adds/removes
- preserved root `AddPIDs(...)` behavior

The likely test file is still
`pkg/appolly/discover/dynamic_pid_selector_test.go`.

### Why this phase exists

This isolates the selector state-model change from app discovery propagation and
from exporter gating.

## Phase 4: Propagate signal ownership through app discovery and lifecycle consumers

### Purpose

Teach the discovery side to understand which dynamic PID owns app signal
selection, especially for child-process inheritance.

### Scope

- use the app-union view for `ProcessWatcher` and `DynamicMatcher`
- carry the controlling selector PID through discovery matches
- persist that signal-owner PID into service metadata
- switch network/stats pipelines from the root selector to their signal-specific
  views

### Behavior after this phase

- traces and app metrics still share app lifecycle
- child processes discovered through a selected parent inherit the correct
  dynamic app-signal ownership
- net/stats now consume `NetworkMetrics()` / `StatsMetrics()` instead of the
  root selector

### Detailed implementation

This phase is about carrying signal ownership through the parts of the app
pipeline that decide which process actually gets instrumented.

`ProcessMatch` grows one field:

```go
type ProcessMatch struct {
    Criteria            []services.Selector
    LogEnricherCriteria []services.Selector
    Process             *services.ProcessInfo
    DynamicSelectorPID  app.PID
}
```

`DynamicMatcher` changes from using the root selector as a generic
`services.Selector` to using the internal app-union view directly:

```go
type DynamicMatcher struct {
    DynamicPIDSelector *dynamicPIDSignalView
    // ...
}
```

Matching records the selector-owned PID:

```go
if m.DynamicPIDSelector.IncludesPID(proc.Pid) {
    return &ProcessMatch{
        Criteria:           []services.Selector{m.DynamicPIDSelector.AsSelector()},
        Process:            proc,
        DynamicSelectorPID: proc.Pid,
    }
}
```

Then parent/child inheritance carries that ownership through `ProcessHistory`.

The selector-owned PID is copied into service metadata:

```go
type Attrs struct {
    ProcPID            app.PID
    DynamicSelectorPID app.PID
    // ...
}
```

and in `makeServiceAttrs(...)`:

```go
s := svc.Attrs{
    ProcPID:            processMatch.Process.Pid,
    DynamicSelectorPID: processMatch.DynamicSelectorPID,
    // ...
}
```

The finder switches from the root selector to the app-union view:

```go
var appDynamicSelector *dynamicPIDSignalView
if startConfig.dynamicPIDSelector != nil {
    appDynamicSelector = startConfig.dynamicPIDSelector.appSignals()
}
```

Then:

```go
addedPIDsCh = appDynamicSelector.AddedPIDsNotify()
swi.Add(dynamicMatcherProvider(langEnrichedEvents, criteriaFilteredEvents, appDynamicSelector))
```

At the same time, non-app pipelines switch to signal-specific views:

```go
dynamicSelector := ctxInfo.DynamicPIDSelector.NetworkMetrics()
```

and:

```go
dynamicSelector := ctxInfo.DynamicPIDSelector.StatsMetrics()
```

This phase should not yet decide whether a span becomes a trace or an app metric.
It only ensures that later gating logic can ask "which dynamic PID owns this
service instance?" and that non-app signals stop consuming the root selector.

### Why this phase exists

This addresses the major correctness risk in the rollout: signal ownership must
survive discovery and parent/child matching before export behavior can safely
split.

## Phase 5: Gate AppO11y traces and app metrics independently

### Purpose

Introduce the actual app-side behavioral split between traces and app metrics.

### Scope

- add a post-decoration span gate that marks traces vs app metrics separately
- add a process-event gate for app-metrics lifecycle consumers
- wire those gates into the AppO11y export path
- update trace-oriented debug/export helpers if needed so they reflect the trace
  view

### Behavior after this phase

- `Traces()` and `AppMetrics()` can be enabled independently
- discovery/instrumentation is still shared through the app-union lifecycle
- exporter behavior and process-event-driven metrics stay consistent

### Detailed implementation

This phase introduces explicit gates in the AppO11y export path rather than
trying to split discovery or tracer attachment.

The new helper file can look like:

```go
func DynamicSignalSpanGate(
    selector selection.MultiSignalPIDSelector,
    input, output *msg.Queue[[]request.Span],
) swarm.InstanceFunc

func DynamicSignalProcessEventGate(
    selector selection.MultiSignalPIDSelector,
    input, output *msg.Queue[exec.ProcessEvent],
) swarm.InstanceFunc
```

The span gate uses the selector-owned PID, not blindly `ProcPID`:

```go
func dynamicSignalPID(span *request.Span) app.PID {
    if span.Service.DynamicSelectorPID != 0 {
        return span.Service.DynamicSelectorPID
    }
    return span.Service.ProcPID
}
```

Then:

```go
if !selector.Traces().IncludesPID(pid) {
    request.SetIgnoreTraces(&spans[i])
}
if !selector.AppMetrics().IncludesPID(pid) {
    request.SetIgnoreMetrics(&spans[i])
}
```

This gate is inserted after the existing app attribute filter:

```go
attrFilteredSpans := msg2.QueueFromConfig[[]request.Span](config, "attrFilteredSpans")
exportableSpans := msg2.QueueFromConfig[[]request.Span](config, "exportableSpans")

swi.Add(filter.ByAttribute(..., nameResolverToAttrFilter, attrFilteredSpans))
swi.Add(DynamicSignalSpanGate(ctxInfo.DynamicPIDSelector, attrFilteredSpans, exportableSpans))
```

Metrics process events need their own gate because exporters like target-info
and service-graph state consume lifecycle events separately from spans:

```go
metricsProcessEvents := msg2.QueueFromConfig[exec.ProcessEvent](config, "metricsProcessEvents")
swi.Add(DynamicSignalProcessEventGate(ctxInfo.DynamicPIDSelector, processEventsCh, metricsProcessEvents))
```

That gated queue then replaces the original process event queue for metrics
exporters:

```go
swi.Add(otel.ReportMetrics(..., spanNameAggregatedMetrics, metricsProcessEvents))
swi.Add(otel.ReportSvcGraphMetrics(..., spanNameAggregatedMetrics, metricsProcessEvents))
swi.Add(prom.PrometheusEndpoint(..., spanNameAggregatedMetrics, metricsProcessEvents))
```

Finally, trace-oriented debug output should reflect the trace view:

```go
func traceVisibleSpans(spans []request.Span) []request.Span {
    out := make([]request.Span, 0, len(spans))
    for i := range spans {
        if request.IgnoreTraces(&spans[i]) {
            continue
        }
        out = append(out, spans[i])
    }
    return out
}
```

This phase is where app traces-only vs app metrics-only behavior becomes real.

### Why this phase exists

This keeps the selector refactor separate from the highest-risk behavioral
change: independent app trace-vs-metrics export.

## Phase 6: Harden non-app signal ownership and overlapping network identity

### Purpose

Finish the non-app side of the model and remove early-deletion edge cases.

### Scope

- ensure `NetworkMetrics()` and `StatsMetrics()` are fully independent in all
  call sites
- add refcounting for derived IP ownership when multiple selected PIDs resolve
  to the same pod IP
- update docs to describe final selector semantics

### Behavior after this phase

- overlapping selected PIDs no longer drop a shared pod IP too early
- all four signal views behave independently
- root compatibility behavior remains documented

### Detailed implementation

The main correctness fix in this phase is changing `DynamicAppIPs` from a plain
IP set to refcounted ownership.

Before:

```go
type DynamicAppIPs struct {
    allowed map[string]struct{}
    pidToIPs map[app.PID][]string
}
```

After:

```go
type DynamicAppIPs struct {
    allowedIPs map[string]int
    pidToIPs   map[app.PID][]string
}
```

Batch add/remove logic becomes:

```go
for _, ip := range ips {
    d.allowedIPs[ip]++
}
```

and:

```go
for _, ip := range ips {
    d.allowedIPs[ip]--
    if d.allowedIPs[ip] <= 0 {
        delete(d.allowedIPs, ip)
    }
}
```

If a PID is re-resolved to a different IP set, the old ownership must be
decremented before incrementing the new one:

```go
if prevIPs, ok := d.pidToIPs[pid]; ok {
    for _, ip := range prevIPs {
        d.allowedIPs[ip]--
        if d.allowedIPs[ip] <= 0 {
            delete(d.allowedIPs, ip)
        }
    }
}
```

This phase should also finalize docs around root compatibility, for example:

```go
selector.AddPIDs(1234) // equivalent to selecting all supported signals
```

and add tests that explicitly cover:

- two selected PIDs sharing one pod IP
- removing one PID does not drop the IP while the other still owns it
- `NetworkMetrics()` and `StatsMetrics()` behave independently from app signals

The likely final test additions belong in
`pkg/selection/dynamic_app_ips_test.go` plus targeted pipeline-level tests if
needed.

### Why this phase exists

This isolates the final cleanup and correctness work for non-app signals from
the larger selector/app rollout.

## Suggested PR sequence

Recommended PR titles:

1. `move dynamic PID selector into shared instrumenter context`
2. `add dynamic metrics filters for network and stats pipelines`
3. `refactor DynamicPIDSelector into signal-aware selector views`
4. `propagate signal-aware selection through discovery and metrics consumers`
5. `gate app traces and metrics independently for dynamic targets`
6. `harden network and stats signal ownership for dynamic selection`

If fewer PRs are preferred, phases 5 and 6 can be combined, but phases 3 and 4
should stay separate because they split selector-state changes from lifecycle
propagation.

## Open decisions to keep explicit during implementation

- During intermediate phases, what should root `AddPIDs(...)` mean exactly:
  app-only compatibility or all known signals?
- Should network/stats subviews be introduced in Phase 3 but only consumed in
  Phase 4, or introduced only when their first consumer is ready?
- When app metrics are disabled for a dynamically selected PID, which
  process-event-driven metrics must stop observing it immediately, and which can
  continue to treat discovery as app-union state?

## Success criteria

The rollout is complete when:

- one selector object can control traces, app metrics, network metrics, and
  stats metrics independently
- traces and app metrics still share one discovery/instrumentation lifecycle
- net/stats no longer depend on app-union behavior
- root `AddPIDs(...)` remains supported and documented
- dynamic selection behavior is covered by focused tests for selector state,
  discovery propagation, app gating, and net/stats IP ownership
