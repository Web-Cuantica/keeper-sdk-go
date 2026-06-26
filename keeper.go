// Package keeper es el SDK de observabilidad de Keeper para Go: trazas, métricas y
// logs "bien armados" sobre OpenTelemetry, exportados por OTLP a la plataforma Keeper
// (SigNoz). Implementa el Keeper Logging Standard (ADR-0014): mensaje legible +
// atributos planos, semántica OTel, correlación por trace y redacción de PII.
package keeper

import (
	"context"
	"errors"
	"fmt"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/url"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otlplog "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otlpmetric "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otlptrace "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// scopeName identifica al SDK como InstrumentationScope de los logs.
const scopeName = "github.com/Web-Cuantica/keeper-sdk-go"

var (
	mu     sync.Mutex
	logger *slog.Logger
)

// Start inicializa Keeper (trazas + métricas + logs) con export OTLP, instala el
// propagador W3C y el logger estructurado. Llamar al inicio del proceso. El shutdown
// devuelto vacía los buffers; llamarlo en el cierre del servicio.
func Start(ctx context.Context, opts ...Option) (func(context.Context) error, error) {
	cfg := resolveConfig(opts...)

	res, err := resource.New(ctx,
		resource.WithHost(),    // host.name (columna HOST en la plataforma)
		resource.WithProcess(), // process.pid, runtime, etc.
		resource.WithAttributes(
			attribute.String("service.name", cfg.service),
			attribute.String("service.version", cfg.version),
			attribute.String("deployment.environment", cfg.env),
			attribute.String("service.instance.id", instanceID()),
		),
	)
	if err != nil {
		// Conflictos de schema entre detectores no son fatales: se usa lo detectado.
		if res == nil {
			return nil, fmt.Errorf("keeper: resource: %w", err)
		}
	}

	host, insecure := endpointParts(cfg.endpoint)

	// --- Trazas ---
	traceOpts := []otlptrace.Option{otlptrace.WithEndpoint(host)}
	if insecure {
		traceOpts = append(traceOpts, otlptrace.WithInsecure())
	}
	traceExp, err := otlptrace.New(ctx, traceOpts...)
	if err != nil {
		return nil, fmt.Errorf("keeper: exporter de trazas: %w", err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExp))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	// --- Métricas ---
	metricOpts := []otlpmetric.Option{otlpmetric.WithEndpoint(host)}
	if insecure {
		metricOpts = append(metricOpts, otlpmetric.WithInsecure())
	}
	metricExp, err := otlpmetric.New(ctx, metricOpts...)
	if err != nil {
		return nil, fmt.Errorf("keeper: exporter de métricas: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	otel.SetMeterProvider(mp)

	// --- Logs ---
	logOpts := []otlplog.Option{otlplog.WithEndpoint(host)}
	if insecure {
		logOpts = append(logOpts, otlplog.WithInsecure())
	}
	logExp, err := otlplog.New(ctx, logOpts...)
	if err != nil {
		return nil, fmt.Errorf("keeper: exporter de logs: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otellog.SetLoggerProvider(lp)

	// slog -> OTel logs (handler propio: fija SeverityText), con request_id de
	// contexto, redacción y nivel mínimo.
	var h slog.Handler = newOtelHandler(lp.Logger(scopeName))
	h = redactHandler{next: h, keys: cfg.redactKeys}
	h = contextHandler{next: h}
	h = &leveledHandler{next: h, level: cfg.level}

	mu.Lock()
	logger = slog.New(h)
	slog.SetDefault(logger)
	mu.Unlock()

	shutdown := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
	}
	return shutdown, nil
}

// Logger devuelve el *slog.Logger de Keeper. Pásale el contexto del request para
// correlacionar el log con la traza activa (trace_id/span_id nativos).
func Logger() *slog.Logger {
	mu.Lock()
	defer mu.Unlock()
	if logger == nil {
		return slog.Default()
	}
	return logger
}

// LogError registra el error en el span activo (RecordError + status Error) y loguea
// a nivel error con los atributos exception.type/exception.message más los que pases.
func LogError(ctx context.Context, msg string, err error, attrs ...slog.Attr) {
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	all := make([]slog.Attr, 0, len(attrs)+2)
	all = append(all,
		slog.String("exception.type", fmt.Sprintf("%T", err)),
		slog.String("exception.message", err.Error()),
	)
	all = append(all, attrs...)
	Logger().LogAttrs(ctx, slog.LevelError, msg, all...)
}

// endpointParts separa un endpoint OTLP "http://host:puerto" en host y si es inseguro.
func endpointParts(raw string) (host string, insecure bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, true
	}
	return u.Host, u.Scheme != "https"
}

// instanceID genera un id único por proceso para service.instance.id.
func instanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
