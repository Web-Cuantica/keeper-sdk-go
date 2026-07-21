// Package keeperfiber provee el middleware de Keeper para Fiber v2: genera/propaga
// request_id, crea el span del request (propagando el trace entrante), emite el log
// HTTP estándar (semántica OTel) y hace recover de panics.
package keeperfiber

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// defaultIgnorePaths: tráfico de plomería que no debe generar span/log (§7 centinela
// "nunca registrar" — ruido/costo sin valor de negocio).
var defaultIgnorePaths = []string{"/health", "/healthz", "/live", "/ready", "/ping", "/metrics"}

// MiddlewareConfig configura el middleware Fiber.
type MiddlewareConfig struct {
	// IgnorePaths son rutas (sufijo o exactas) que NO generan telemetría.
	// nil ⇒ defaultIgnorePaths; slice vacío ⇒ no se ignora ninguna.
	IgnorePaths []string
}

// Middleware instala observabilidad por request en una app Fiber:
//   - request_id (lee el header entrante o genera un ULID) en contexto y respuesta;
//   - span de servidor propagando el trace context entrante (W3C);
//   - log "request completed" con semántica HTTP de OTel;
//   - recover de panics (500 + log de error).
//
// Acepta opcionalmente MiddlewareConfig (p. ej. Middleware(MiddlewareConfig{IgnorePaths: ...})).
func Middleware(cfg ...MiddlewareConfig) fiber.Handler {
	ignore := defaultIgnorePaths
	if len(cfg) > 0 && cfg[0].IgnorePaths != nil {
		ignore = cfg[0].IgnorePaths
	}
	tracer := otel.Tracer(scopeName)
	prop := otel.GetTextMapPropagator()

	return func(c *fiber.Ctx) error {
		safePath := keeper.SafeUTF8(c.Path())
		if shouldIgnorePath(safePath, ignore) {
			return c.Next()
		}

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
		// Acumulador del evento ancho canónico: el código de negocio anota atributos con
		// keeper.Annotate(ctx, ...) y aquí se vuelcan al span y al log de cierre (§3.1).
		ctx = keeper.ContextWithEvent(ctx)
		c.Set(RequestIDHeader, rid)

		ctx, span := tracer.Start(ctx, c.Method()+" "+safePath,
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
			attribute.String("url.path", safePath),
			attribute.Int("http.response.status_code", status),
			// sample_rate: cuántos requests representa este span (1 si no hay muestreo).
			// Permite reponderar conteos/percentiles en el análisis (§7.2).
			attribute.Float64("sample_rate", keeper.SampleRate()),
		)
		// Evento ancho canónico: vuelca los atributos de negocio acumulados al span
		// (redactados; el span no pasa por el handler de redacción de logs) (§3.1/§4.1).
		eventAttrs := keeper.EventAttrs(ctx)
		if len(eventAttrs) > 0 {
			span.SetAttributes(slogAttrsToOtel(keeper.RedactAttrs(eventAttrs))...)
		}
		// Plantilla de ruta (ya resuelta tras c.Next()) como http.route, y como
		// nombre del span (semconv HTTP: "{método} {http.route}") — agrupa por
		// operación sin explotar en cardinalidad por los ids de la URL.
		rutaLegible := safePath
		if route := c.Route().Path; route != "" && route != "/" {
			ruta := keeper.SafeUTF8(route)
			span.SetAttributes(attribute.String("http.route", ruta))
			span.SetName(c.Method() + " " + ruta)
			rutaLegible = ruta
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
		// Los atributos de negocio acumulados se agregan al evento de cierre (el
		// handler de logs los redacta). El span ya los lleva (arriba).
		logAttrs := []slog.Attr{
			slog.String("http.request.method", c.Method()),
			slog.String("url.path", safePath),
			slog.Int("http.response.status_code", status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.Float64("sample_rate", keeper.SampleRate()),
		}
		logAttrs = append(logAttrs, eventAttrs...)
		keeper.Logger().LogAttrs(ctx, levelFor(status), mensajeDeCierre(c.Method(), rutaLegible, status), logAttrs...)
		return nextErr
	}
}

// mensajeDeCierre construye el cuerpo del log de cierre del request: método, ruta
// (plantilla si existe) y una razón legible por status — p. ej.
// "GET /api/v1/fleet/:id → 404 no encontrado" — en lugar del genérico
// "request completed", que obligaba a leer los atributos para saber qué pasó.
// La URL concreta y el resto del contexto siguen en los atributos (url.path, etc.).
func mensajeDeCierre(method, ruta string, status int) string {
	return fmt.Sprintf("%s %s → %d %s", method, ruta, status, razonHTTP(status))
}

// razonHTTP traduce el status a una razón corta en el idioma de los logs del
// estándar. Los casos enumerados son los que el negocio ve a diario; el resto
// cae en la familia.
func razonHTTP(status int) string {
	switch status {
	case fiber.StatusBadRequest:
		return "solicitud inválida"
	case fiber.StatusUnauthorized:
		return "no autenticado"
	case fiber.StatusForbidden:
		return "acceso denegado"
	case fiber.StatusNotFound:
		return "no encontrado"
	case fiber.StatusMethodNotAllowed:
		return "método no permitido"
	case fiber.StatusConflict:
		return "conflicto"
	case fiber.StatusUnprocessableEntity:
		return "datos no procesables"
	case fiber.StatusTooManyRequests:
		return "límite de peticiones excedido"
	case fiber.StatusServiceUnavailable:
		return "servicio no disponible"
	}
	switch {
	case status >= 500:
		return "error del servidor"
	case status >= 400:
		return "error del cliente"
	case status >= 300:
		return "redirección"
	default:
		return "ok"
	}
}

// shouldIgnorePath indica si la ruta es tráfico de plomería (health/metrics).
// Igualdad o sufijo (las entradas de ignore empiezan con '/', así que
// /api/v1/health matchea /health y /unhealthy no).
func shouldIgnorePath(path string, ignore []string) bool {
	for _, p := range ignore {
		if p == "" {
			continue
		}
		if path == p || strings.HasSuffix(path, p) {
			return true
		}
	}
	return false
}

// levelFor mapea el status HTTP al nivel del log "request completed":
//   - 5xx → Error: fallo del servidor, siempre se registra.
//   - 404 → Debug: "no encontrado" es casi siempre cliente o escáneres de internet
//     sondeando la IP pública (/.env, /.git/config, …); no es una señal accionable del
//     servidor. En producción (nivel info) NO se exporta, evitando el ruido de los bots.
//   - resto de 4xx (400/401/403/409/422…) → Warn: fallo a nivel HTTP que sí interesa ver.
//   - <400 (éxito) → Debug: en producción NO se exporta. El evento de negocio ya lo
//     cubren los logs semánticos de cada handler ("Cliente creado", etc.) y la traza;
//     así se evita el ruido de un "request completed" duplicado por request.
func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status == 404:
		return slog.LevelDebug
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelDebug
	}
}

// slogAttrsToOtel convierte atributos slog a atributos de span OTel, saneando UTF-8 en
// los strings (un byte inválido haría que el exportador rechace el lote).
func slogAttrsToOtel(attrs []slog.Attr) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		switch a.Value.Kind() {
		case slog.KindBool:
			out = append(out, attribute.Bool(a.Key, a.Value.Bool()))
		case slog.KindInt64:
			out = append(out, attribute.Int64(a.Key, a.Value.Int64()))
		case slog.KindUint64:
			out = append(out, attribute.Int64(a.Key, int64(a.Value.Uint64())))
		case slog.KindFloat64:
			out = append(out, attribute.Float64(a.Key, a.Value.Float64()))
		case slog.KindString:
			out = append(out, attribute.String(a.Key, keeper.SafeUTF8(a.Value.String())))
		default:
			out = append(out, attribute.String(a.Key, keeper.SafeUTF8(a.Value.String())))
		}
	}
	return out
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
