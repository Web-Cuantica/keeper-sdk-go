package keeper

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientKey
)

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
