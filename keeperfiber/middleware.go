// Package keeperfiber provee el middleware de Keeper para Fiber v2: genera/propaga
// request_id, crea el span del request (propagando el trace entrante), emite el log
// HTTP estándar (semántica OTel) y hace recover de panics.
package keeperfiber

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	keeper "github.com/Web-Cuantica/keeper-sdk-go"
)

// RequestIDHeader es el header de correlación que se lee/escribe.
const RequestIDHeader = "X-Request-ID"

const scopeName = "github.com/Web-Cuantica/keeper-sdk-go/keeperfiber"

// Middleware instala observabilidad por request en una app Fiber:
//   - request_id (lee el header entrante o genera un ULID) en contexto y respuesta;
//   - span de servidor propagando el trace context entrante (W3C);
//   - log "request completed" con semántica HTTP de OTel;
//   - recover de panics (500 + log de error).
func Middleware() fiber.Handler {
	tracer := otel.Tracer(scopeName)
	prop := otel.GetTextMapPropagator()

	return func(c *fiber.Ctx) error {
		ctx := prop.Extract(c.UserContext(), fiberCarrier{c})

		rid := c.Get(RequestIDHeader)
		if rid == "" {
			rid = ulid.Make().String()
		}
		ctx = keeper.ContextWithRequestID(ctx, rid)
		c.Set(RequestIDHeader, rid)

		route := c.Route().Path
		if route == "" {
			route = c.Path()
		}
		ctx, span := tracer.Start(ctx, c.Method()+" "+route,
			trace.WithSpanKind(trace.SpanKindServer))
		c.SetUserContext(ctx)
		start := time.Now()

		var nextErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic: %v", r)
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					keeper.Logger().ErrorContext(ctx, "panic recuperado en handler",
						slog.Any("panic", r))
					_ = c.SendStatus(fiber.StatusInternalServerError)
				}
			}()
			nextErr = c.Next()
		}()

		status := c.Response().StatusCode()
		span.SetAttributes(
			attribute.String("http.request.method", c.Method()),
			attribute.String("url.path", c.Path()),
			attribute.Int("http.response.status_code", status),
		)
		if status >= 500 {
			span.SetStatus(codes.Error, "")
		}
		span.End()

		keeper.Logger().LogAttrs(ctx, levelFor(status), "request completed",
			slog.String("http.request.method", c.Method()),
			slog.String("url.path", c.Path()),
			slog.Int("http.response.status_code", status),
			slog.String("client.address", c.IP()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
		return nextErr
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// fiberCarrier adapta los headers del request de Fiber a TextMapCarrier de OTel
// para extraer el trace context entrante (traceparent).
type fiberCarrier struct{ c *fiber.Ctx }

func (f fiberCarrier) Get(key string) string { return f.c.Get(key) }
func (f fiberCarrier) Set(key, val string)   { f.c.Set(key, val) }
func (f fiberCarrier) Keys() []string {
	var keys []string
	f.c.Request().Header.VisitAll(func(k, _ []byte) {
		keys = append(keys, string(k))
	})
	return keys
}
