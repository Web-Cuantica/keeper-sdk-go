package keeper

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// clavesDeError son las claves con las que el código Go suele adjuntar un error a un log.
var clavesDeError = map[string]struct{}{"error": {}, "err": {}}

// erroresHandler convierte el atributo convencional de Go (`"error", err`) en exception.type y
// exception.message, y en los logs de nivel error marca el span activo. Así un servicio que ya
// loguea sus errores a la manera de Go queda con la semántica de OpenTelemetry sin reescribir
// cada llamada: es lo que hace keeper.LogError, aplicado a lo que ya existe.
type erroresHandler struct{ next slog.Handler }

func (h erroresHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h erroresHandler) Handle(ctx context.Context, r slog.Record) error {
	hay := false
	r.Attrs(func(a slog.Attr) bool {
		_, hay = clavesDeError[a.Key]
		return !hay
	})
	if !hay {
		return h.next.Handle(ctx, r)
	}
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if _, ok := clavesDeError[a.Key]; !ok {
			nr.AddAttrs(a)
			return true
		}
		convertidos, err := convertirError(a)
		nr.AddAttrs(convertidos...)
		if err != nil && r.Level >= slog.LevelError {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
		return true
	})
	return h.next.Handle(ctx, nr)
}

func (h erroresHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if _, ok := clavesDeError[a.Key]; ok {
			convertidos, _ := convertirError(a)
			out = append(out, convertidos...)
			continue
		}
		out = append(out, a)
	}
	return erroresHandler{next: h.next.WithAttrs(out)}
}

func (h erroresHandler) WithGroup(name string) slog.Handler {
	return erroresHandler{next: h.next.WithGroup(name)}
}

// convertirError devuelve los atributos exception.* del valor. Un error nil o un texto vacío no
// dejan nada: "sin error" no es un dato. Devuelve además el error, si el valor lo era.
func convertirError(a slog.Attr) ([]slog.Attr, error) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindAny {
		if err, ok := v.Any().(error); ok && err != nil {
			return []slog.Attr{
				slog.String("exception.type", fmt.Sprintf("%T", err)),
				slog.String("exception.message", err.Error()),
			}, err
		}
		if v.Any() == nil {
			return nil, nil
		}
	}
	if texto := v.String(); texto != "" {
		return []slog.Attr{slog.String("exception.message", texto)}, nil
	}
	return nil, nil
}
