package keeper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Captura guarda en memoria lo que el servicio emitiría, ya filtrado por el contrato y censurado.
// Con ella una prueba puede afirmar que un dato personal no aparece en ningún span ni log: es la
// prueba de contrato con datos, complemento de Violaciones().
type Captura struct {
	spans *tracetest.InMemoryExporter
	mu    sync.Mutex
	logs  []LogCapturado
}

// LogCapturado es un log tal como saldría hacia Keeper.
type LogCapturado struct {
	Nivel     slog.Level
	Mensaje   string
	Atributos map[string]string
}

// NuevaCaptura crea una captura vacía.
func NuevaCaptura() *Captura {
	return &Captura{spans: tracetest.NewInMemoryExporter()}
}

// WithCaptura manda la telemetría a la captura en lugar de exportarla. Implica WithSinExportar:
// las trazas se procesan en síncrono y nada sale del proceso.
func WithCaptura(c *Captura) Option {
	return func(cfg *config) {
		cfg.captura = c
		cfg.sinExportar = true
	}
}

// Spans devuelve los spans capturados.
func (c *Captura) Spans() []tracetest.SpanStub { return c.spans.GetSpans() }

// Logs devuelve una copia de los logs capturados.
func (c *Captura) Logs() []LogCapturado {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]LogCapturado(nil), c.logs...)
}

// Limpiar vacía la captura.
func (c *Captura) Limpiar() {
	c.spans.Reset()
	c.mu.Lock()
	c.logs = nil
	c.mu.Unlock()
}

// Buscar devuelve dónde aparece el texto: nombres y atributos de spans y de sus eventos,
// descripciones de estado, mensajes y atributos de logs. Vacío si no aparece en ningún lado.
func (c *Captura) Buscar(texto string) []string {
	var donde []string
	en := func(valor, lugar string) {
		if strings.Contains(valor, texto) {
			donde = append(donde, lugar)
		}
	}
	enAtributos := func(attrs []attribute.KeyValue, lugar string) {
		for _, kv := range attrs {
			en(kv.Value.Emit(), fmt.Sprintf("%s, atributo %s", lugar, kv.Key))
		}
	}
	for _, s := range c.Spans() {
		lugar := fmt.Sprintf("span %q", s.Name)
		en(s.Name, lugar+", nombre")
		en(s.Status.Description, lugar+", estado")
		enAtributos(s.Attributes, lugar)
		for _, ev := range s.Events {
			en(ev.Name, fmt.Sprintf("%s, evento %q", lugar, ev.Name))
			enAtributos(ev.Attributes, fmt.Sprintf("%s, evento %q", lugar, ev.Name))
		}
	}
	for _, l := range c.Logs() {
		lugar := fmt.Sprintf("log %q", l.Mensaje)
		en(l.Mensaje, lugar+", mensaje")
		for k, v := range l.Atributos {
			en(v, fmt.Sprintf("%s, atributo %s", lugar, k))
		}
	}
	return donde
}

// capturaSlog guarda cada log en la captura, con los grupos aplanados como prefijo.
type capturaSlog struct {
	c     *Captura
	fijos []slog.Attr
	grupo string
}

func (h capturaSlog) Enabled(context.Context, slog.Level) bool { return true }

func (h capturaSlog) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	for _, a := range h.fijos {
		aplanar(attrs, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		aplanar(attrs, h.grupo, a)
		return true
	})
	h.c.mu.Lock()
	h.c.logs = append(h.c.logs, LogCapturado{Nivel: r.Level, Mensaje: r.Message, Atributos: attrs})
	h.c.mu.Unlock()
	return nil
}

func (h capturaSlog) WithAttrs(attrs []slog.Attr) slog.Handler {
	fijos := append([]slog.Attr(nil), h.fijos...)
	for _, a := range attrs {
		if h.grupo != "" {
			a.Key = h.grupo + a.Key
		}
		fijos = append(fijos, a)
	}
	return capturaSlog{c: h.c, fijos: fijos, grupo: h.grupo}
}

func (h capturaSlog) WithGroup(name string) slog.Handler {
	return capturaSlog{c: h.c, fijos: h.fijos, grupo: h.grupo + name + "."}
}

func aplanar(dst map[string]string, prefijo string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, g := range v.Group() {
			aplanar(dst, prefijo+a.Key+".", g)
		}
		return
	}
	dst[prefijo+a.Key] = v.String()
}

// abanico manda el mismo log a varios handlers.
type abanico []slog.Handler

func (a abanico) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range a {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (a abanico) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, h := range a {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("abanico de logs: %v", errs)
	}
	return nil
}

func (a abanico) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(abanico, len(a))
	for i, h := range a {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (a abanico) WithGroup(name string) slog.Handler {
	out := make(abanico, len(a))
	for i, h := range a {
		out[i] = h.WithGroup(name)
	}
	return out
}
