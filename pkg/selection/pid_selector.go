// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package selection

import "go.opentelemetry.io/obi/pkg/appolly/app"

// PIDSelector is the runtime PID set API used to restrict metrics export to selected applications.
type PIDSelector interface {
	GetPIDs() ([]app.PID, bool)
	AddedPIDsNotify() <-chan []app.PID
	RemovedNotify() <-chan []app.PID
}
