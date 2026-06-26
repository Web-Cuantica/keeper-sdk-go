package keeper

import (
	"context"
	"log/slog"
	"time"

	otellog "go.opentelemetry.io/otel/log"
)

// otelHandler es un slog.Handler que emite log records de OpenTelemetry fijando
// tanto la severidad numérica (SeverityNumber) como el texto (SeverityText:
// TRACE/DEBUG/INFO/WARN/ERROR/FATAL), para que la plataforma muestre y filtre el
// nivel. Reemplaza al bridge otelslog (que dejaba SeverityText vacío). La
// correlación con la traza la añade el SDK de logs a partir del contexto.
type otelHandler struct {
	logger otellog.Logger
	attrs  []otellog.KeyValue
}

func newOtelHandler(logger otellog.Logger) *otelHandler {
	return &otelHandler{logger: logger}
}

// El filtrado por nivel lo hace leveledHandler; aquí siempre habilitado.
func (h *otelHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *otelHandler) Handle(ctx context.Context, r slog.Record) error {
	var rec otellog.Record
	if r.Time.IsZero() {
		rec.SetTimestamp(time.Now())
	} else {
		rec.SetTimestamp(r.Time)
	}
	rec.SetBody(otellog.StringValue(r.Message))
	sev, text := mapSeverity(r.Level)
	rec.SetSeverity(sev)
	rec.SetSeverityText(text)
	rec.AddAttributes(h.attrs...)
	r.Attrs(func(a slog.Attr) bool {
		rec.AddAttributes(attrToKeyValue(a))
		return true
	})
	h.logger.Emit(ctx, rec)
	return nil
}

func (h *otelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	na := make([]otellog.KeyValue, len(h.attrs), len(h.attrs)+len(attrs))
	copy(na, h.attrs)
	for _, a := range attrs {
		na = append(na, attrToKeyValue(a))
	}
	return &otelHandler{logger: h.logger, attrs: na}
}

func (h *otelHandler) WithGroup(string) slog.Handler { return h }

// mapSeverity traduce el nivel de slog a la severidad de OTel (número + texto).
func mapSeverity(l slog.Level) (otellog.Severity, string) {
	switch {
	case l <= slog.LevelDebug-4:
		return otellog.SeverityTrace, "TRACE"
	case l <= slog.LevelDebug:
		return otellog.SeverityDebug, "DEBUG"
	case l <= slog.LevelInfo:
		return otellog.SeverityInfo, "INFO"
	case l <= slog.LevelWarn:
		return otellog.SeverityWarn, "WARN"
	case l <= slog.LevelError:
		return otellog.SeverityError, "ERROR"
	default:
		return otellog.SeverityFatal, "FATAL"
	}
}

func attrToKeyValue(a slog.Attr) otellog.KeyValue {
	switch a.Value.Kind() {
	case slog.KindBool:
		return otellog.Bool(a.Key, a.Value.Bool())
	case slog.KindInt64:
		return otellog.Int64(a.Key, a.Value.Int64())
	case slog.KindUint64:
		return otellog.Int64(a.Key, int64(a.Value.Uint64()))
	case slog.KindFloat64:
		return otellog.Float64(a.Key, a.Value.Float64())
	case slog.KindString:
		return otellog.String(a.Key, a.Value.String())
	case slog.KindDuration:
		return otellog.String(a.Key, a.Value.Duration().String())
	case slog.KindTime:
		return otellog.String(a.Key, a.Value.Time().Format(time.RFC3339Nano))
	default:
		return otellog.String(a.Key, a.Value.String())
	}
}
