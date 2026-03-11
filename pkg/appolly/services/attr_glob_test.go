// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/appolly/app"
)

func TestGlobAttributes_GetPIDs_empty(t *testing.T) {
	ga := &GlobAttributes{Name: "svc", Namespace: "ns"}
	pids, ok := ga.GetPIDs()
	assert.False(t, ok)
	assert.Nil(t, pids)
}

func TestGlobAttributes_GetPIDs_static(t *testing.T) {
	ga := &GlobAttributes{Name: "svc", Namespace: "ns", PIDs: []uint32{1, 2, 3}}
	pids, ok := ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{1, 2, 3}, pids)
}
