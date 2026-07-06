// Package keeperfiber provee el middleware de Keeper para Fiber v2: genera/propaga
// request_id, crea el span del request (propagando el trace entrante), emite el log
// HTTP estándar (semántica OTel) y hace recover de panics.
package keeperfiber

import (
	"errors"
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
			// Reutiliza el id del middleware requestid de Fiber si ya corrió.
			if v, ok := c.Locals("requestid").(string); ok && v != "" {
				rid = v
			}
		}
		if rid == "" {
			rid = ulid.Make().String()
		}
		ctx = keeper.ContextWithRequestID(ctx, rid)
		// Origen del request (IP + navegador/SO/dispositivo) para todos los logs.
		ctx = keeper.ContextWithClient(ctx, keeper.ParseClient(c.IP(), c.Get("User-Agent")))
		c.Set(RequestIDHeader, rid)

		ctx, span := tracer.Start(ctx, c.Method()+" "+c.Path(),
			trace.WithSpanKind(trace.SpanKindServer))
		c.SetUserContext(ctx)
		start := time.Now()

		var nextErr error
		panicked := false
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicked = true
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

		// Status efectivo: si el handler DEVOLVIÓ un error, Fiber aún no lo tradujo
		// a la respuesta (su error handler corre después del middleware), así que lo
		// inferimos del error para atribuir bien el span y contar la excepción.
		status := c.Response().StatusCode()
		if nextErr != nil {
			var fe *fiber.Error
			if errors.As(nextErr, &fe) {
				status = fe.Code
			} else {
				status = fiber.StatusInternalServerError // error genérico -> 500 en Fiber
			}
		}

		span.SetAttributes(
			attribute.String("http.request.method", c.Method()),
			attribute.String("url.path", c.Path()),
			attribute.Int("http.response.status_code", status),
		)
		// Plantilla de ruta (ya resuelta tras c.Next()) como http.route.
		if route := c.Route().Path; route != "" && route != "/" {
			span.SetAttributes(attribute.String("http.route", route))
		}
		// Excepciones: todo 5xx se registra como excepción del span (aparece en la
		// vista Excepciones de Keeper), salvo que el recover de un panic ya lo haya
		// hecho. Se usa el error devuelto por el handler si lo hay; si no, se
		// sintetiza uno con el status. Los <500 no generan excepción (evita ruido).
		switch {
		case panicked:
			// La excepción ya se registró en el recover; no duplicar.
		case status >= 500:
			err := nextErr
			if err == nil {
				err = fmt.Errorf("respuesta HTTP %d", status)
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()

		// client.address/browser/os/device los inyecta el contextHandler del SDK
		// desde el contexto (ver ContextWithClient arriba), no se repiten aquí.
		keeper.Logger().LogAttrs(ctx, levelFor(status), "request completed",
			slog.String("http.request.method", c.Method()),
			slog.String("url.path", c.Path()),
			slog.Int("http.response.status_code", status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
		return nextErr
	}
}

// levelFor mapea el status HTTP al nivel del log "request completed":
//   - 5xx → Error, 4xx → Warn: son la señal de fallo a nivel HTTP y siempre se registran.
//   - <400 (éxito) → Debug: en producción (nivel info) NO se exporta. El evento de
//     negocio ya lo cubren los logs semánticos de cada handler ("Cliente creado", etc.)
//     y la traza; así se evita el ruido de un "request completed" duplicado por request.
func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelDebug
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
