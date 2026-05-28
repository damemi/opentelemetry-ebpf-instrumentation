// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tracesgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
	trace2 "go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/obi/pkg/appolly/app/request"
	attr "go.opentelemetry.io/obi/pkg/export/attributes/names"
)

func TestTraceAttributesSelector_SunRPCClient(t *testing.T) {
	span := &request.Span{
		Type:      request.EventTypeSunRPCClient,
		Method:    "3",
		Path:      "nfs",
		Statement: "rpcsec_gss v4",
		Host:      "10.0.0.2",
		HostPort:  2049,
		Peer:      "10.0.0.1",
	}

	attrs := TraceAttributesSelector(span, map[attr.Name]struct{}{})

	require.NotEmpty(t, attrs)
	assert.Equal(t, trace2.SpanKindClient, spanKind(span))
	assert.Contains(t, attrs, semconv.RPCSystemKey.String("sunrpc"))
	assert.Contains(t, attrs, semconv.RPCService("nfs"))
	assert.Contains(t, attrs, semconv.RPCMethod("3"))
	assert.Contains(t, attrs, attribute.String("sunrpc.auth.flavor", "rpcsec_gss v4"))
}

func TestTraceAttributesSelector_SunRPCErrorStatus(t *testing.T) {
	span := &request.Span{
		Type:   request.EventTypeSunRPCClient,
		Method: "1",
		Path:   "mount",
		Status: 3,
	}

	attrs := TraceAttributesSelector(span, map[attr.Name]struct{}{})
	assert.Contains(t, attrs, attribute.Int(string(attr.RPCResponseStatusCode), 3))
}

func TestSpanKind_SunRPCServer(t *testing.T) {
	span := &request.Span{Type: request.EventTypeSunRPCServer}
	assert.Equal(t, trace2.SpanKindServer, spanKind(span))
}
