package keeper

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// HeaderSetter es el contrato mínimo para inyectar headers en una llamada saliente
// (http.Header, map[string]string, clientes HTTP propios, etc.).
type HeaderSetter interface {
	Set(key, value string)
}

// headerCarrier adapta HeaderSetter a TextMapCarrier de OTel.
type headerCarrier struct{ set HeaderSetter }

func (c headerCarrier) Get(string) string { return "" }
func (c headerCarrier) Set(k, v string)   { c.set.Set(k, v) }
func (c headerCarrier) Keys() []string    { return nil }

// InjectOutbound propaga el contexto de traza W3C (`traceparent`) y el `X-Request-ID`
// legible en una llamada saliente (§4.1 MUST). Usar antes de cada request HTTP/gRPC
// saliente cuando no se usa un cliente instrumentado automáticamente.
//
//	req, _ := http.NewRequestWithContext(ctx, ...)
//	keeper.InjectOutbound(ctx, req.Header)
func InjectOutbound(ctx context.Context, headers HeaderSetter) {
	if headers == nil {
		return
	}
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier{set: headers})
	if rid := RequestID(ctx); rid != "" {
		headers.Set("X-Request-ID", rid)
	}
}

// Compila-time assert: TraceContext existe en el SDK (propagación W3C).
var _ propagation.TextMapPropagator = propagation.TraceContext{}
