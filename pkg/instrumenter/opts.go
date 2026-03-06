// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter // import "go.opentelemetry.io/obi/pkg/instrumenter"

import (
	"go.opentelemetry.io/obi/pkg/appolly/app"
	"go.opentelemetry.io/obi/pkg/appolly/app/request"
	"go.opentelemetry.io/obi/pkg/pipe/global"
	"go.opentelemetry.io/obi/pkg/pipe/msg"
)

// Option that override the instantiation of the instrumenter
type Option func(info *global.ContextInfo)

// TargetPIDsUpdater is implemented by the default App O11y instrumenter. It allows adding,
// removing, and inspecting target PIDs at runtime. Works with any config; no initial
// target_pids required.
type TargetPIDsUpdater interface {
	AddTargetPIDs(pids ...int)
	RemoveTargetPIDs(pids ...int)
	TargetPIDs() []app.PID
}

// WithTargetPIDsUpdater calls the given function with a TargetPIDsUpdater after the App O11y
// instrumenter is created. Callers receive the App O11y instrumenter (as this interface); store it
// and call AddTargetPIDs/RemoveTargetPIDs at runtime to change which PIDs are instrumented, or
// TargetPIDs to inspect the currently tracked set.
func WithTargetPIDsUpdater(onReady func(TargetPIDsUpdater)) Option {
	return func(info *global.ContextInfo) {
		info.AppO11y.OnTargetPIDsUpdaterReady = func(v any) {
			if u, ok := v.(TargetPIDsUpdater); ok {
				onReady(u)
			}
		}
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
