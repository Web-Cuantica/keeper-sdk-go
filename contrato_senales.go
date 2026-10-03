package keeper

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func attrString(k, v string) attribute.KeyValue { return attribute.String(k, v) }

// --- Logs ---

// contratoHandler aplica el contrato a los atributos de cada log, incluidos los que
// se fijan con logger.With. Va antes de la redacción: lo que no sale no se procesa.
type contratoHandler struct {
	next slog.Handler
	c    *contratoActivo
}

func (h contratoHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h contratoHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if h.c.admite(a.Key, senalLogs) {
			nr.AddAttrs(a)
		}
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h contratoHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if h.c.admite(a.Key, senalLogs) {
			out = append(out, a)
		}
	}
	return contratoHandler{next: h.next.WithAttrs(out), c: h.c}
}

func (h contratoHandler) WithGroup(name string) slog.Handler {
	return contratoHandler{next: h.next.WithGroup(name), c: h.c}
}

// --- Trazas ---

// exportadorConContrato filtra los atributos de cada span y de sus eventos antes de
// exportar, y convierte en hash los identificadores. Cubre también los atributos que
// ponen las librerías de instrumentación, que no pasan por ningún handler de logs.
type exportadorConContrato struct {
	next sdktrace.SpanExporter
	c    *contratoActivo
}

func (e exportadorConContrato) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = e.filtrar(s)
	}
	return e.next.ExportSpans(ctx, out)
}

func (e exportadorConContrato) Shutdown(ctx context.Context) error { return e.next.Shutdown(ctx) }

func (e exportadorConContrato) filtrar(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	attrs, cambioAttrs := e.filtrarAtributos(s.Attributes())
	eventos := s.Events()
	cambioEventos := false
	for i, ev := range eventos {
		fa, cambio := e.filtrarAtributos(ev.Attributes)
		if !cambio {
			continue
		}
		if !cambioEventos {
			eventos = append([]sdktrace.Event(nil), eventos...)
			cambioEventos = true
		}
		eventos[i].Attributes = fa
	}
	if !cambioAttrs && !cambioEventos {
		return s
	}
	return spanFiltrado{ReadOnlySpan: s, atributos: attrs, eventos: eventos}
}

func (e exportadorConContrato) filtrarAtributos(in []attribute.KeyValue) ([]attribute.KeyValue, bool) {
	out := in
	cambio := false
	for i, kv := range in {
		clave := string(kv.Key)
		admitida := e.c.admite(clave, senalTrazas)
		reemplazo, esIdent := kv, e.c.esIdentificador(clave)
		if esIdent {
			reemplazo = attribute.String(clave, protegerIdentificador(kv.Value.String()))
		}
		if admitida && !esIdent {
			if cambio {
				out = append(out, kv)
			}
			continue
		}
		if !cambio {
			out = append(make([]attribute.KeyValue, 0, len(in)), in[:i]...)
			cambio = true
		}
		if admitida {
			out = append(out, reemplazo)
		}
	}
	return out, cambio
}

// protegerIdentificador devuelve el hash h1:… con el pepper configurado, o la
// censura si no hay pepper: un identificador nunca sale en claro.
func protegerIdentificador(valor string) string {
	if pepper := getHashPepper(); pepper != "" {
		if h := hashIDWithPepper(pepper, valor); h != "" {
			return h
		}
	}
	return redactCensor
}

// spanFiltrado presenta un span con atributos y eventos ya filtrados. Embebe el span
// original para heredar el resto de la interfaz, incluido su método privado.
type spanFiltrado struct {
	sdktrace.ReadOnlySpan
	atributos []attribute.KeyValue
	eventos   []sdktrace.Event
}

func (s spanFiltrado) Attributes() []attribute.KeyValue { return s.atributos }
func (s spanFiltrado) Events() []sdktrace.Event         { return s.eventos }

// exportadorNulo descarta todo: con WithSinExportar el contrato se sigue aplicando
// y contando, pero nada sale del proceso.
type exportadorNulo struct{}

func (exportadorNulo) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (exportadorNulo) Shutdown(context.Context) error                             { return nil }

// --- Métricas ---

// vistaConContrato aplica el contrato a los atributos de todas las métricas. Los
// identificadores no pueden ir en métricas: se quitan siempre, declarados o no.
func vistaConContrato(c *contratoActivo) sdkmetric.View {
	return sdkmetric.NewView(sdkmetric.Instrument{Name: "*"}, sdkmetric.Stream{
		AttributeFilter: func(kv attribute.KeyValue) bool {
			clave := string(kv.Key)
			if c.esIdentificador(clave) {
				return false
			}
			return c.admite(clave, senalMetricas)
		},
	})
}
