package keeper

import (
	"context"
	"log/slog"
	"sync"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientKey
	eventKey
)

// eventAccumulator junta los atributos del evento ancho canónico del request
// (el "blob vacío" del §3.1): el código de negocio los anota durante el request y
// el middleware los vuelca (al span y al log de cierre) al terminar la unidad de trabajo.
type eventAccumulator struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

// ContextWithEvent inicia el acumulador del evento canónico del request. Lo llama el
// middleware HTTP al comenzar la unidad de trabajo.
func ContextWithEvent(ctx context.Context) context.Context {
	return context.WithValue(ctx, eventKey, &eventAccumulator{})
}

// Annotate añade atributos al evento canónico del request presente en el contexto
// (§3.1: acumula, no fragmentes). Fuera de un request instrumentado es un no-op seguro.
func Annotate(ctx context.Context, attrs ...slog.Attr) {
	if acc, ok := ctx.Value(eventKey).(*eventAccumulator); ok {
		acc.mu.Lock()
		acc.attrs = append(acc.attrs, attrs...)
		acc.mu.Unlock()
	}
}

// AnnotateUser anota el usuario del request (semconv OTel `enduser.id`, §4.1/§3.2).
func AnnotateUser(ctx context.Context, userID string) {
	if userID == "" {
		return
	}
	Annotate(ctx, slog.String("enduser.id", userID))
}

// AnnotateTenant anota el tenant del request (`tenant.id`, §3.2).
func AnnotateTenant(ctx context.Context, tenantID string) {
	if tenantID == "" {
		return
	}
	Annotate(ctx, slog.String("tenant.id", tenantID))
}

// AnnotateOutcome registra el éxito/fallo de negocio (no solo HTTP) del request (§3.2).
// Un HTTP 200 con business.success=false es un evento malo para SLIs.
func AnnotateOutcome(ctx context.Context, success bool, errKind, errMsg string) {
	attrs := []slog.Attr{slog.Bool("business.success", success)}
	if errKind != "" {
		attrs = append(attrs, slog.String("error.kind", errKind))
	}
	if errMsg != "" {
		attrs = append(attrs, slog.String("error.message", errMsg))
	}
	Annotate(ctx, attrs...)
}

// EventAttrs devuelve una copia de los atributos acumulados del evento canónico.
func EventAttrs(ctx context.Context) []slog.Attr {
	if acc, ok := ctx.Value(eventKey).(*eventAccumulator); ok {
		acc.mu.Lock()
		defer acc.mu.Unlock()
		out := make([]slog.Attr, len(acc.attrs))
		copy(out, acc.attrs)
		return out
	}
	return nil
}

// ContextWithRequestID guarda el id de correlación en el contexto. Lo usa el
// middleware HTTP; cada log emitido con ese contexto incluye `request_id`.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID devuelve el id de correlación del contexto, o "" si no hay.
func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithClient guarda el origen del request (IP + dispositivo) en el
// contexto. Lo usa el middleware HTTP; cada log emitido con ese contexto incluye
// los atributos client.* (address/browser/os/device).
func ContextWithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, clientKey, c)
}

// ClientFromContext devuelve el origen del request del contexto.
func ClientFromContext(ctx context.Context) (Client, bool) {
	c, ok := ctx.Value(clientKey).(Client)
	return c, ok
}
