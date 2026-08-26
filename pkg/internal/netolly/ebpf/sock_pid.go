// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package ebpf // import "go.opentelemetry.io/obi/pkg/internal/netolly/ebpf"

import (
	"errors"
	"io"
	"log/slog"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

type sockFlowPIDPrograms struct {
	tcpConnect *ebpf.Program
	tcpSendmsg *ebpf.Program
	udpSendmsg *ebpf.Program
	accept     *ebpf.Program
	tcpClose   *ebpf.Program
}

func attachSockFlowPIDProbes(log *slog.Logger, progs sockFlowPIDPrograms) []io.Closer {
	var closables []io.Closer
	for _, p := range []struct {
		name string
		ret  bool
		prog *ebpf.Program
	}{
		{name: "tcp_connect", prog: progs.tcpConnect},
		{name: "tcp_sendmsg", prog: progs.tcpSendmsg},
		{name: "udp_sendmsg", prog: progs.udpSendmsg},
		{name: "inet_csk_accept", ret: true, prog: progs.accept},
		{name: "tcp_close", prog: progs.tcpClose},
	} {
		if p.prog == nil {
			continue
		}
		var (
			l   link.Link
			err error
		)
		if p.ret {
			l, err = link.Kretprobe(p.name, p.prog, nil)
		} else {
			l, err = link.Kprobe(p.name, p.prog, nil)
		}
		if err != nil {
			log.Warn("can't attach netolly sock PID probe; bare-host PID attribution may be incomplete",
				"hook", p.name, "error", err)
			continue
		}
		closables = append(closables, l)
	}
	return closables
}

func setSelectedNetPID(m *ebpf.Map, pid uint32, selected bool) error {
	if m == nil {
		return nil
	}
	if selected {
		v := uint8(1)
		return m.Put(pid, v)
	}
	err := m.Delete(pid)
	if errors.Is(err, ebpf.ErrKeyNotExist) {
		return nil
	}
	return err
}
