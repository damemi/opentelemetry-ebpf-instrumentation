// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ebpfcommon // import "go.opentelemetry.io/obi/pkg/ebpf/common"

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"go.opentelemetry.io/obi/pkg/appolly/app"
	"go.opentelemetry.io/obi/pkg/appolly/app/request"
	"go.opentelemetry.io/obi/pkg/internal/largebuf"
	"go.opentelemetry.io/obi/pkg/internal/sunrpcparser"
)

// SunRPCInfo carries parsed ONC RPC metadata for span creation.
type SunRPCInfo struct {
	Program     uint32
	Version     uint32
	Procedure   uint32
	ProgramName string
	Method      string
	AuthFlavor  string
	Status      int
}

func ProcessPossibleSunRPCEvent(event *TCPRequestInfo, pkt, rpkt *largebuf.LargeBuffer) (*SunRPCInfo, bool, error) {
	reqInfo, reqIgnore, reqErr := processSunRPCBuffer(pkt)
	respInfo, respIgnore, respErr := processSunRPCBuffer(rpkt)

	reqCall := reqErr == nil && !reqIgnore && isSunRPCCallInfo(reqInfo)
	respCall := respErr == nil && !respIgnore && isSunRPCCallInfo(respInfo)

	switch {
	case reqCall:
		mergeSunRPCReplyStatus(reqInfo, respInfo, respErr, respIgnore)
		return reqInfo, false, nil
	case respCall:
		reverseTCPEvent(event)
		mergeSunRPCReplyStatus(respInfo, reqInfo, reqErr, reqIgnore)
		return respInfo, false, nil
	case reqErr == nil && !reqIgnore && reqInfo != nil:
		return reqInfo, false, nil
	case respErr == nil && !respIgnore && respInfo != nil:
		reverseTCPEvent(event)
		return respInfo, false, nil
	case reqErr == nil || respErr == nil:
		return nil, true, nil
	}

	if errors.Is(reqErr, sunrpcparser.ErrNotSunRPC) && errors.Is(respErr, sunrpcparser.ErrNotSunRPC) {
		return nil, true, sunrpcparser.ErrNotSunRPC
	}

	return nil, true, errors.Join(reqErr, respErr)
}

func isSunRPCCallInfo(info *SunRPCInfo) bool {
	return info != nil && info.Method != "reply"
}

func mergeSunRPCReplyStatus(callInfo *SunRPCInfo, replyInfo *SunRPCInfo, replyErr error, replyIgnore bool) {
	if callInfo == nil || replyErr != nil || replyIgnore || replyInfo == nil {
		return
	}
	if replyInfo.Status != 0 {
		callInfo.Status = replyInfo.Status
	}
}

func processSunRPCBuffer(pkt *largebuf.LargeBuffer) (*SunRPCInfo, bool, error) {
	if pkt == nil || pkt.IsEmpty() {
		return nil, true, sunrpcparser.ErrNotSunRPC
	}

	reader := pkt.NewReader()
	if !sunrpcparser.IsLikelySunRPC(&reader) {
		return nil, true, sunrpcparser.ErrNotSunRPC
	}

	reader = pkt.NewReader()
	result, err := sunrpcparser.Parse(&reader)
	if err != nil {
		if errors.Is(err, sunrpcparser.ErrNotSunRPC) {
			return nil, true, err
		}
		return nil, true, err
	}

	if result.Call != nil {
		return sunRPCInfoFromCall(result.Call, result.Reply), false, nil
	}

	if result.Reply != nil {
		return sunRPCInfoFromReply(result.Reply), false, nil
	}

	return nil, true, nil
}

func sunRPCInfoFromCall(call *sunrpcparser.CallInfo, reply *sunrpcparser.ReplyInfo) *SunRPCInfo {
	progName := sunrpcparser.ProgramName(call.Program)
	if progName == "" {
		progName = fmt.Sprintf("%d", call.Program)
	}

	info := &SunRPCInfo{
		Program:     call.Program,
		Version:     call.Version,
		Procedure:   call.Procedure,
		ProgramName: progName,
		Method:      sunrpcparser.ProcedureLabel(call.Program, call.Procedure),
		AuthFlavor:  sunrpcparser.AuthFlavorName(call.AuthFlavor),
	}

	if reply != nil && reply.MatchCallXid {
		if reply.AcceptStat != sunrpcAcceptSuccess {
			info.Status = int(reply.AcceptStat) + 1
		}
	}

	return info
}

func sunRPCInfoFromReply(reply *sunrpcparser.ReplyInfo) *SunRPCInfo {
	info := &SunRPCInfo{
		ProgramName: "sunrpc",
		Method:      "reply",
	}
	if reply.AcceptStat != sunrpcAcceptSuccess {
		info.Status = int(reply.AcceptStat) + 1
	}
	return info
}

const sunrpcAcceptSuccess = 0

func TCPToSunRPCToSpan(trace *TCPRequestInfo, data *SunRPCInfo) request.Span {
	peer := ""
	hostname := ""
	peerPort := 0
	hostPort := 0

	if trace.ConnInfo.S_port != 0 || trace.ConnInfo.D_port != 0 {
		peer, hostname = (*BPFConnInfo)(&trace.ConnInfo).reqHostInfo()
		peerPort = int(trace.ConnInfo.S_port)
		hostPort = int(trace.ConnInfo.D_port)
	}

	spanType := sunRPCSpanType(trace, data)

	var subType int
	if data.Version > 0 && data.Version <= 255 {
		subType = int(data.Version)
	}

	return request.Span{
		Type:          spanType,
		Method:        data.Method,
		Path:          data.ProgramName,
		Route:         strconv.FormatUint(uint64(data.Procedure), 10),
		Statement:     data.AuthFlavor,
		SubType:       subType,
		Peer:          peer,
		PeerPort:      peerPort,
		Host:          hostname,
		HostPort:      hostPort,
		RequestStart:  int64(trace.StartMonotimeNs),
		Start:         int64(trace.StartMonotimeNs),
		End:           int64(trace.EndMonotimeNs),
		Status:        data.Status,
		TraceID:       trace.Tp.TraceId,
		SpanID:        trace.Tp.SpanId,
		ParentSpanID:  trace.Tp.ParentId,
		TraceFlags:    trace.Tp.Flags,
		Pid: request.PidInfo{
			HostPID:   app.PID(trace.Pid.HostPid),
			UserPID:   app.PID(trace.Pid.UserPid),
			Namespace: trace.Pid.Ns,
		},
	}
}

func sunRPCSpanType(trace *TCPRequestInfo, data *SunRPCInfo) request.EventType {
	// For CALL spans, recv on server and send on client (same as NATS/MQTT/Redis).
	serverOnRecv := trace.Direction == directionRecv
	// Reply-only spans (missed CALL leg): Direction reflects the REPLY leg, so invert.
	if !isSunRPCCallInfo(data) {
		serverOnRecv = !serverOnRecv
	}
	if serverOnRecv {
		return request.EventTypeSunRPCServer
	}
	return request.EventTypeSunRPCClient
}

func matchSunRPC(_ *EBPFParseContext, event *TCPRequestInfo, requestBuffer, responseBuffer *largebuf.LargeBuffer) (request.Span, bool, bool, error) { //nolint:unparam
	info, ignore, err := ProcessPossibleSunRPCEvent(event, requestBuffer, responseBuffer)
	if ignore && err == nil {
		return request.Span{}, true, true, nil
	}

	if err != nil {
		if errors.Is(err, sunrpcparser.ErrNotSunRPC) {
			return request.Span{}, false, false, nil
		}
		slog.Debug("SunRPC parsing failed after heuristic match, dropping event", "error", err)
		return request.Span{}, true, true, nil
	}

	if info == nil {
		return request.Span{}, true, true, nil
	}

	return TCPToSunRPCToSpan(event, info), false, true, nil
}
