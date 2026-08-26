// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpf // import "go.opentelemetry.io/obi/pkg/internal/netolly/ebpf"

import (
	"github.com/cilium/ebpf"
)

// lookupAndDeletePidFlowMap reads aggregated_flows_pid. The iterator path is used
// instead of batch lookup: this map is only populated for selected bare-host PIDs.
func lookupAndDeletePidFlowMap(flowMap *ebpf.Map, lastReadNS uint64) (map[NetPidFlowId]*NetFlowMetrics, uint64, error) {
	if flowMap == nil {
		return nil, 0, nil
	}

	flows := map[NetPidFlowId]*NetFlowMetrics{}
	oldestFlow := uint64(0)

	id := NetPidFlowId{}
	var metrics []NetFlowMetrics
	for iterator := flowMap.Iterate(); iterator.Next(&id, &metrics); {
		if err := flowMap.Delete(id); err != nil {
			// best-effort: a concurrent CPU may have already removed the entry
		}

		perCPUAggregated := &NetFlowMetrics{}
		for i := range metrics {
			mt := &metrics[i]
			if mt.StartMonoTimeNs <= lastReadNS || mt.EndMonoTimeNs <= lastReadNS {
				continue
			}
			perCPUAggregated.Accumulate(mt)
			oldestFlow = max(oldestFlow, mt.EndMonoTimeNs)
		}
		if perCPUAggregated.EndMonoTimeNs == 0 {
			continue
		}

		if stored, ok := flows[id]; ok {
			stored.Accumulate(perCPUAggregated)
		} else {
			flows[id] = perCPUAggregated
		}
		metrics = nil
	}
	return flows, oldestFlow, nil
}
