// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package services // import "go.opentelemetry.io/obi/pkg/appolly/services"

import (
	"iter"

	"go.opentelemetry.io/obi/pkg/appolly/app"
	"go.opentelemetry.io/obi/pkg/export/otel/perapp"
)

// PidSelector selects a single process by PID for instrumentation.
// When used, only the process with the given PID is matched (path and port checks are skipped).
type PidSelector struct {
	Pid       app.PID
	Name      string
	Namespace string
}

func (p *PidSelector) GetName() string                        { return p.Name }
func (p *PidSelector) GetNamespace() string                   { return p.Namespace }
func (p *PidSelector) GetPath() StringMatcher                 { return nilMatcher{} }
func (p *PidSelector) GetPathRegexp() StringMatcher           { return nilMatcher{} }
func (p *PidSelector) GetOpenPorts() *PortEnum                { return &PortEnum{} }
func (p *PidSelector) GetPID() (app.PID, bool)                { return p.Pid, true }
func (p *PidSelector) IsContainersOnly() bool                 { return false }
func (p *PidSelector) GetExportModes() ExportModes            { return ExportModes{} }
func (p *PidSelector) GetSamplerConfig() *SamplerConfig       { return nil }
func (p *PidSelector) GetRoutesConfig() *CustomRoutesConfig   { return nil }
func (p *PidSelector) MetricsConfig() perapp.SvcMetricsConfig { return perapp.SvcMetricsConfig{} }

func (p *PidSelector) RangeMetadata() iter.Seq2[string, StringMatcher] {
	return func(_ func(string, StringMatcher) bool) {}
}

func (p *PidSelector) RangePodLabels() iter.Seq2[string, StringMatcher] {
	return func(_ func(string, StringMatcher) bool) {}
}

func (p *PidSelector) RangePodAnnotations() iter.Seq2[string, StringMatcher] {
	return func(_ func(string, StringMatcher) bool) {}
}
