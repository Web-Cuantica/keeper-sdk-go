package keeper

import (
	"context"
	"log/slog"
	"strings"
)

// leveledHandler aplica un nivel mínimo sobre el handler subyacente.
type leveledHandler struct {
	next  slog.Handler
	level slog.Level
}

func (h *leveledHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= h.level && h.next.Enabled(ctx, l)
}

func (h *leveledHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.next.Handle(ctx, r)
}

func (h *leveledHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &leveledHandler{next: h.next.WithAttrs(attrs), level: h.level}
}

func (h *leveledHandler) WithGroup(name string) slog.Handler {
	return &leveledHandler{next: h.next.WithGroup(name), level: h.level}
}

// redactHandler censura los atributos cuya clave sea sensible (PII/secrets) antes
// de pasarlos al handler subyacente (el bridge a OTel logs).
type redactHandler struct {
	next slog.Handler
	keys map[string]struct{}
}

func (h redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(h.redact(a))
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.redact(a)
	}
	return redactHandler{next: h.next.WithAttrs(out), keys: h.keys}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{next: h.next.WithGroup(name), keys: h.keys}
}

func (h redactHandler) redact(a slog.Attr) slog.Attr {
	if _, ok := h.keys[strings.ToLower(a.Key)]; ok {
		return slog.String(a.Key, redactCensor)
	}
	return a
}
