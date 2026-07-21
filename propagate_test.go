package keeper

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type mapHeaders map[string]string

func (m mapHeaders) Set(k, v string) { m[k] = v }

func TestInjectOutboundPropagaTraceYRequestID(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	tp := sdktrace.NewTracerProvider()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	ctx = ContextWithRequestID(ctx, "rid-abc")

	h := mapHeaders{}
	InjectOutbound(ctx, h)

	if h["traceparent"] == "" {
		t.Errorf("falta traceparent: %v", h)
	}
	if h["X-Request-ID"] != "rid-abc" {
		t.Errorf("X-Request-ID = %q", h["X-Request-ID"])
	}
}

func TestInjectOutboundNilEsNoop(t *testing.T) {
	InjectOutbound(context.Background(), nil) // no debe panicar
}

func TestInjectOutboundSinRequestIDSoloTrace(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	h := mapHeaders{}
	InjectOutbound(ctx, h)
	if h["traceparent"] == "" {
		t.Error("falta traceparent")
	}
	if _, ok := h["X-Request-ID"]; ok {
		t.Error("no debía inyectar X-Request-ID vacío")
	}
}
