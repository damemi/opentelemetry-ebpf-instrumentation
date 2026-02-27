// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package appolly

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/appolly/app"
	"go.opentelemetry.io/obi/pkg/appolly/discover"
	"go.opentelemetry.io/obi/pkg/appolly/discover/exec"
	"go.opentelemetry.io/obi/pkg/ebpf"
	"go.opentelemetry.io/obi/pkg/export/connector"
	"go.opentelemetry.io/obi/pkg/export/otel/otelcfg"
	"go.opentelemetry.io/obi/pkg/obi"
	"go.opentelemetry.io/obi/pkg/pipe/global"
)

func TestProcessEventsLoopDoesntBlock(t *testing.T) {
	instr, err := New(
		t.Context(),
		&global.ContextInfo{
			Prometheus: &connector.PrometheusManager{},
		},
		&obi.Config{
			ChannelBufferLen: 1,
			Traces: otelcfg.TracesConfig{
				TracesEndpoint: "http://something",
			},
		},
	)

	events := make(chan discover.Event[*ebpf.Instrumentable])

	go instr.instrumentedEventLoop(t.Context(), events)

	for i := range app.PID(100) {
		events <- discover.Event[*ebpf.Instrumentable]{
			Obj:  &ebpf.Instrumentable{FileInfo: &exec.FileInfo{Pid: i}},
			Type: discover.EventCreated,
		}
	}

	assert.NoError(t, err)
}

// targetPIDsUpdater is the same as instrumenter.TargetPIDsUpdater; used to avoid import cycle.
type targetPIDsUpdater interface {
	AddTargetPIDs(pids ...int)
	RemoveTargetPIDs(pids ...int)
}

func TestInstrumenter_ImplementsTargetPIDsUpdater(t *testing.T) {
	instr, err := New(
		t.Context(),
		&global.ContextInfo{Prometheus: &connector.PrometheusManager{}},
		&obi.Config{ChannelBufferLen: 1, Traces: otelcfg.TracesConfig{TracesEndpoint: "http://localhost"}},
	)
	require.NoError(t, err)
	var _ targetPIDsUpdater = instr
}

func TestInstrumenter_AddTargetPIDs_RemoveTargetPIDs(t *testing.T) {
	instr, err := New(
		t.Context(),
		&global.ContextInfo{Prometheus: &connector.PrometheusManager{}},
		&obi.Config{ChannelBufferLen: 1, Traces: otelcfg.TracesConfig{TracesEndpoint: "http://localhost"}},
	)
	require.NoError(t, err)

	// AddTargetPIDs and RemoveTargetPIDs should not panic; instrumenter always has pidSelector
	instr.AddTargetPIDs(1, 2, 3)
	instr.AddTargetPIDs(2, 4) // 2 duplicate, 4 new
	instr.RemoveTargetPIDs(2)
	instr.RemoveTargetPIDs(99) // not present, no-op
	instr.AddTargetPIDs()      // no-op
	instr.RemoveTargetPIDs()   // no-op
}
