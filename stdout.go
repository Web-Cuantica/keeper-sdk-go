package keeper

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// WithLogStdout copia cada log, ya filtrado y censurado, a la salida estándar en
// JSON con trace_id y span_id. Sin ella los logs solo viajan a Keeper y
// `docker logs` queda vacío. También: KEEPER_LOG_STDOUT=true.
func WithLogStdout() Option {
	return func(c *config) { c.logStdout = true }
}

// WithSinExportar aplica todo el pipeline (contrato, censura, niveles) sin mandar
// nada fuera del proceso. Pensado para pruebas de contrato y desarrollo sin Keeper.
func WithSinExportar() Option {
	return func(c *config) { c.sinExportar = true }
}

// dobleSalida manda el mismo log a Keeper y a un escritor local en JSON.
type dobleSalida struct {
	otel  slog.Handler
	local slog.Handler
}

func nuevaDobleSalida(otel slog.Handler, w io.Writer) dobleSalida {
	// El nivel lo decide leveledHandler antes; aquí se deja pasar todo.
	local := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug - 4})
	return dobleSalida{otel: otel, local: local}
}

func (d dobleSalida) Enabled(ctx context.Context, l slog.Level) bool {
	return d.otel.Enabled(ctx, l) || d.local.Enabled(ctx, l)
}

func (d dobleSalida) Handle(ctx context.Context, r slog.Record) error {
	errOtel := d.otel.Handle(ctx, r)
	// En la consola no hay correlación nativa: se agregan los ids de la traza para
	// poder ir de `docker logs` a la traza en Keeper.
	local := r
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		local = r.Clone()
		local.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return errors.Join(errOtel, d.local.Handle(ctx, local))
}

func (d dobleSalida) WithAttrs(attrs []slog.Attr) slog.Handler {
	return dobleSalida{otel: d.otel.WithAttrs(attrs), local: d.local.WithAttrs(attrs)}
}

func (d dobleSalida) WithGroup(name string) slog.Handler {
	return dobleSalida{otel: d.otel.WithGroup(name), local: d.local.WithGroup(name)}
}
