// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package discover // import "go.opentelemetry.io/obi/pkg/appolly/discover"

import (
	"sync"

	"go.opentelemetry.io/obi/pkg/appolly/app"
)

// DynamicPIDSelector holds the runtime set of target PIDs for OBI. It is preloaded from
// config target_pids and updated at runtime via AddPIDs/RemovePIDs. Only the discover
// matcher uses it for matching; the instrumenter (or appolly) holds a reference and
// calls AddPIDs/RemovePIDs directly.
type DynamicPIDSelector struct {
	mu        sync.RWMutex
	pids      []uint32
	removedCh chan []app.PID // owned by selector; RemovedNotify() returns receive-only view
}

// NewDynamicPIDSelector creates a new dynamic PID selector (initially empty).
func NewDynamicPIDSelector() *DynamicPIDSelector {
	return &DynamicPIDSelector{
		removedCh: make(chan []app.PID, 1),
	}
}

// RemovedNotify returns the channel on which removed PIDs are sent when RemovePIDs is called.
// The matcher uses this to emit synthetic deletes. Safe to call from multiple goroutines.
func (d *DynamicPIDSelector) RemovedNotify() <-chan []app.PID {
	return d.removedCh
}

// GetPIDs returns a copy of the current PID list and true when non-empty.
func (d *DynamicPIDSelector) GetPIDs() ([]app.PID, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.pids) == 0 {
		return nil, false
	}
	out := make([]app.PID, len(d.pids))
	for i, p := range d.pids {
		out[i] = app.PID(p)
	}
	return out, true
}

// AddPIDs adds PIDs to the set (deduplicated).
func (d *DynamicPIDSelector) AddPIDs(pids ...uint32) {
	if len(pids) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	existing := make(map[uint32]struct{}, len(d.pids))
	for _, p := range d.pids {
		existing[p] = struct{}{}
	}
	for _, u := range pids {
		if _, ok := existing[u]; !ok {
			existing[u] = struct{}{}
			d.pids = append(d.pids, u)
		}
	}
}

// RemovePIDs removes PIDs from the set and sends them on RemovedNotify() for the matcher.
func (d *DynamicPIDSelector) RemovePIDs(pids ...uint32) {
	if len(pids) == 0 {
		return
	}
	toRemove := make(map[uint32]struct{})
	for _, u := range pids {
		toRemove[u] = struct{}{}
	}
	d.mu.Lock()
	newPids := d.pids[:0]
	removedPIDs := make([]app.PID, 0, len(pids))
	for _, p := range d.pids {
		if _, remove := toRemove[p]; !remove {
			newPids = append(newPids, p)
			continue
		}
		removedPIDs = append(removedPIDs, app.PID(p))
	}
	d.pids = newPids
	d.mu.Unlock()
	d.notifyRemoved(removedPIDs)
}

func (d *DynamicPIDSelector) notifyRemoved(removedPIDs []app.PID) {
	if len(removedPIDs) == 0 {
		return
	}
	d.removedCh <- removedPIDs
}
