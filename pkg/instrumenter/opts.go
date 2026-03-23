// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter // import "go.opentelemetry.io/obi/pkg/instrumenter"

import (
	"go.opentelemetry.io/obi/pkg/appolly/app/request"
	"go.opentelemetry.io/obi/pkg/appolly/discover"
	"go.opentelemetry.io/obi/pkg/pipe/global"
	"go.opentelemetry.io/obi/pkg/pipe/msg"
)

// Option that override the instantiation of the instrumenter
type Option func(info *global.ContextInfo)

// WithDynamicPIDSelector passes the given dynamic PID selector into the App O11y pipeline. The caller
// creates it with discover.NewDynamicPIDSelector(), passes it here, and then calls AddPIDs/RemovePIDs/GetPIDs
// on it directly—no callback or reference to the instrumenter is needed.
func WithDynamicPIDSelector(sel *discover.DynamicPIDSelector) Option {
	return func(info *global.ContextInfo) {
		info.AppO11y.DynamicPIDSelector = sel
	}
}

// WithPinIncomingTraceMap pins OBI's shared incoming_trace_map under the configured Otel BPFFS path
// (typically <BPFFSPath>/otel/obi/incoming_trace_map) when the map is first created. Other agents in
// the same process (e.g. Go auto-instrumentation) can open it with ebpf.LoadPinnedMap on that path.
// Requires a working BPFFS mount; failures are logged at debug and do not fail instrumenter startup.
func WithPinIncomingTraceMap() Option {
	return func(info *global.ContextInfo) {
		info.AppO11y.PinIncomingTraceMap = true
	}
}

// OverrideAppExportQueue allows to override the queue used to export the spans.
// This is useful to run the instrumenter in vendored mode, and you want to provide your
// own spans exporter.
// This queue will be used also by other bundled exported (OTEL, Prometheus...) if
// they are configured to run.
// See examples/vendoring/vendoring.go for an example of invocation.
func OverrideAppExportQueue(q *msg.Queue[[]request.Span]) Option {
	return func(info *global.ContextInfo) {
		info.OverrideAppExportQueue = q
	}
}
