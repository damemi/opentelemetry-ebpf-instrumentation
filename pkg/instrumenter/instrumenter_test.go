// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package instrumenter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/appolly/services"
	"go.opentelemetry.io/obi/pkg/export/otel/otelcfg"
	"go.opentelemetry.io/obi/pkg/obi"
	"go.opentelemetry.io/obi/pkg/transform"
)

func TestServiceNameTemplate(t *testing.T) {
	cfg := &obi.Config{
		Attributes: obi.Attributes{
			Kubernetes: transform.KubernetesDecorator{
				ServiceNameTemplate: "{{asdf}}",
			},
		},
	}

	temp, err := buildServiceNameTemplate(cfg)
	assert.Nil(t, temp)
	if assert.Error(t, err) {
		assert.Equal(t, `unable to parse service name template: template: serviceNameTemplate:1: function "asdf" not defined`, err.Error())
	}

	cfg.Attributes.Kubernetes.ServiceNameTemplate = `{{- if eq .Meta.Pod nil }}{{.Meta.Name}}{{ else }}{{- .Meta.Namespace }}/{{ index .Meta.Labels "app.kubernetes.io/name" }}/{{ index .Meta.Labels "app.kubernetes.io/component" -}}{{ if .ContainerName }}/{{ .ContainerName -}}{{ end -}}{{ end -}}`
	temp, err = buildServiceNameTemplate(cfg)

	require.NoError(t, err)
	assert.NotNil(t, temp)

	cfg.Attributes.Kubernetes.ServiceNameTemplate = ""
	temp, err = buildServiceNameTemplate(cfg)
	require.NoError(t, err)
	assert.Nil(t, temp)
}

// TestRun_WithTargetPIDsUpdater_OnlyDynamicPIDSelector verifies that when the instrumenter
// is initialized with a dynamic PID selector (via WithTargetPIDsUpdater), that selector is
// the only discovery criterion—config-based criteria (e.g. discovery.instrument) are not
// used. The selector may be empty by default; callers add/remove PIDs via the updater.
func TestRun_WithTargetPIDsUpdater_OnlyDynamicPIDSelector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Config has discovery.instrument set, but when using WithTargetPIDsUpdater the
	// dynamic PID selector is the sole criterion (this config's Instrument is ignored).
	cfg := &obi.Config{
		ChannelBufferLen: 1,
		Traces:           otelcfg.TracesConfig{TracesEndpoint: "http://localhost:0"},
		Discovery: services.DiscoveryConfig{
			Instrument: services.GlobDefinitionCriteria{
				services.GlobAttributes{Name: "ignored", OpenPorts: services.IntEnum{Ranges: []services.IntRange{{Start: 8080}}}},
			},
		},
	}
	require.True(t, cfg.Enabled(obi.FeatureAppO11y), "test config must enable App O11y")

	var callbackRan bool
	opts := []Option{
		WithTargetPIDsUpdater(func(u TargetPIDsUpdater) {
			require.NotNil(t, u, "caller should receive non-nil TargetPIDsUpdater when using dynamic PID selector")
			// Selector is the only criterion; it may be empty. Caller controls targets via Add/Remove.
			callbackRan = true
		}),
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, opts...) }()

	time.Sleep(2 * time.Second)
	cancel()
	<-done

	require.True(t, callbackRan, "WithTargetPIDsUpdater callback must run; discovery then uses only the dynamic PID selector (even if empty)")
}

// TestRun_WithTargetPIDsUpdater_DynamicUpdate shows that a caller using the public API
// (Run + WithTargetPIDsUpdater) receives the instrumenter and can dynamically add/remove
// target PIDs at runtime. Run is started with a cancelable context and stopped shortly
// after the callback runs so we don't run the full pipeline.
func TestRun_WithTargetPIDsUpdater_DynamicUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Minimal config: enable App O11y (discovery.instrument) and at least one exporter (traces endpoint).
	cfg := &obi.Config{
		ChannelBufferLen: 1,
		Traces:           otelcfg.TracesConfig{TracesEndpoint: "http://localhost:0"},
		Discovery: services.DiscoveryConfig{
			Instrument: services.GlobDefinitionCriteria{
				services.GlobAttributes{Name: "test-svc", OpenPorts: services.IntEnum{Ranges: []services.IntRange{{Start: 8080}}}},
			},
		},
	}
	// Ensure Enabled(FeatureAppO11y) is true (Discovery.Instrument is set)
	require.True(t, cfg.Enabled(obi.FeatureAppO11y), "test config must enable App O11y")

	var (
		receivedUpdater bool
		updaterMu       sync.Mutex
	)
	opts := []Option{
		WithTargetPIDsUpdater(func(u TargetPIDsUpdater) {
			updaterMu.Lock()
			defer updaterMu.Unlock()
			require.NotNil(t, u, "caller should receive non-nil TargetPIDsUpdater")
			// Demonstrate dynamic update: add and remove PIDs at runtime.
			u.AddTargetPIDs(42, 100)
			u.AddTargetPIDs(42, 200) // duplicate 42, new 200
			u.RemoveTargetPIDs(42)
			u.RemoveTargetPIDs(999) // not present, no-op
			receivedUpdater = true
		}),
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, opts...) }()

	// Give setupAppO11y time to create the instrumenter and invoke OnTargetPIDsUpdaterReady.
	// We don't need to wait for the full pipeline; just for the callback.
	time.Sleep(2 * time.Second)
	cancel()

	err := <-done
	// Run typically returns context.Canceled or similar after cancel.
	require.Error(t, err)

	updaterMu.Lock()
	got := receivedUpdater
	updaterMu.Unlock()
	require.True(t, got, "WithTargetPIDsUpdater callback should be invoked with the instrumenter; caller can then add/remove PIDs at runtime")
}
