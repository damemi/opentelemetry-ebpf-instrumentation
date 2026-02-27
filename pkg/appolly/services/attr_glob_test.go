// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/appolly/app"
)

func TestGlobAttributes_AddPIDs_RemovePIDs_GetPIDs(t *testing.T) {
	ga := &GlobAttributes{Name: "svc", Namespace: "ns"}

	// Initially empty
	pids, ok := ga.GetPIDs()
	assert.False(t, ok)
	assert.Nil(t, pids)

	// Add PIDs
	ga.AddPIDs(1, 2, 3)
	pids, ok = ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{1, 2, 3}, pids)

	// Add duplicates and new: no duplicates added
	ga.AddPIDs(2, 3, 4)
	pids, ok = ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{1, 2, 3, 4}, pids)

	// Remove PIDs
	ga.RemovePIDs(2, 4)
	pids, ok = ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{1, 3}, pids)

	// Remove all
	ga.RemovePIDs(1, 3)
	pids, ok = ga.GetPIDs()
	assert.False(t, ok)
	assert.Nil(t, pids)

	// Add after empty
	ga.AddPIDs(42)
	pids, ok = ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{42}, pids)
}

func TestGlobAttributes_AddPIDs_RemovePIDs_emptyNoOp(t *testing.T) {
	ga := &GlobAttributes{PIDs: []uint32{1, 2, 3}}

	// No-op when called with no args
	ga.AddPIDs()
	ga.RemovePIDs()

	pids, ok := ga.GetPIDs()
	require.True(t, ok)
	assert.Equal(t, []app.PID{1, 2, 3}, pids)
}
