package keeper

import (
	"context"
	"log/slog"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
)

func TestEndpointParts(t *testing.T) {
	casos := []struct {
		raw          string
		wantHost     string
		wantInsecure bool
	}{
		{"http://host:4318", "host:4318", true},
		{"https://host:4318", "host:4318", false},
		{"http://localhost:4318", "localhost:4318", true},
		{"localhost:4318", "localhost:4318", true}, // sin esquema => inseguro, raw
	}
	for _, c := range casos {
		host, insecure := endpointParts(c.raw)
		if host != c.wantHost || insecure != c.wantInsecure {
			t.Errorf("endpointParts(%q) = (%q,%v), quería (%q,%v)", c.raw, host, insecure, c.wantHost, c.wantInsecure)
		}
	}
}

func TestMapSeverity(t *testing.T) {
	casos := []struct {
		level slog.Level
		text  string
	}{
		{slog.LevelDebug - 4, "TRACE"},
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
		{slog.LevelError + 4, "FATAL"},
	}
	for _, c := range casos {
		if _, text := mapSeverity(c.level); text != c.text {
			t.Errorf("mapSeverity(%v) texto = %q, quería %q", c.level, text, c.text)
		}
	}
}

func TestAttrToKeyValue(t *testing.T) {
	if kv := attrToKeyValue(slog.Bool("b", true)); kv.Value.AsBool() != true {
		t.Error("bool")
	}
	if kv := attrToKeyValue(slog.Int64("i", 7)); kv.Value.AsInt64() != 7 {
		t.Error("int64")
	}
	if kv := attrToKeyValue(slog.Uint64("u", 9)); kv.Value.AsInt64() != 9 {
		t.Error("uint64")
	}
	if kv := attrToKeyValue(slog.Float64("f", 1.5)); kv.Value.AsFloat64() != 1.5 {
		t.Error("float64")
	}
	if kv := attrToKeyValue(slog.String("s", "hola")); kv.Value.AsString() != "hola" {
		t.Error("string")
	}
	if kv := attrToKeyValue(slog.Duration("d", 2*time.Second)); kv.Value.Kind() != otellog.KindString {
		t.Error("duration debe serializarse a string")
	}
	if kv := attrToKeyValue(slog.Time("t", time.Now())); kv.Value.Kind() != otellog.KindString {
		t.Error("time debe serializarse a string")
	}
	// string con UTF-8 inválido se sanea (no rompe el export OTLP).
	if kv := attrToKeyValue(slog.String("bad", "a\xffb")); kv.Value.AsString() != "ab" {
		t.Errorf("string inválido no saneado: %q", kv.Value.AsString())
	}
}

func TestContextHandlerInyecta(t *testing.T) {
	rec := &recordingHandler{}
	h := contextHandler{next: rec}
	ctx := ContextWithRequestID(context.Background(), "rid-1")
	ctx = ContextWithClient(ctx, Client{Address: "1.2.3.4", Browser: "Chrome", OS: "Windows", DeviceType: "desktop", UserAgent: "UA"})

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	if err := h.Handle(ctx, r); err != nil {
		t.Fatal(err)
	}
	attrs := collectAttrs(rec.rec)
	if attrs["request_id"].String() != "rid-1" {
		t.Errorf("request_id no inyectado: %v", attrs["request_id"])
	}
	if attrs["client.address"].String() != "1.2.3.4" {
		t.Errorf("client.address no inyectado: %v", attrs["client.address"])
	}
	if attrs["user_agent.original"].String() != "UA" {
		t.Errorf("user_agent.original no inyectado")
	}
}

func TestLeveledHandlerFiltra(t *testing.T) {
	rec := &recordingHandler{}
	h := &leveledHandler{next: rec, level: slog.LevelWarn}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info no debe estar habilitado con nivel warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("error debe estar habilitado con nivel warn")
	}
}

func TestInstanceID(t *testing.T) {
	id := instanceID()
	if len(id) != 16 { // 8 bytes en hex
		t.Errorf("instanceID longitud = %d, quería 16 (%q)", len(id), id)
	}
	if id2 := instanceID(); id == id2 {
		t.Error("instanceID debería variar entre llamadas")
	}
}

func TestLogErrorAtributos(t *testing.T) {
	rec := &recordingHandler{}
	prev := logger
	logger = slog.New(rec)
	defer func() { logger = prev }()

	LogError(context.Background(), "fallo al aprobar", context.DeadlineExceeded, slog.Int("rcpt_id", 1))
	attrs := collectAttrs(rec.rec)
	if attrs["exception.message"].String() == "" {
		t.Error("falta exception.message")
	}
	if _, ok := attrs["exception.type"]; !ok {
		t.Error("falta exception.type")
	}
	if attrs["rcpt_id"].Int64() != 1 {
		t.Error("atributo extra no propagado")
	}

	// err nil no debe entrar en pánico.
	LogError(context.Background(), "sin error", nil)
}

func TestDefaultLevelForEnv(t *testing.T) {
	if defaultLevelForEnv("development") != slog.LevelDebug {
		t.Error("development => debug")
	}
	if defaultLevelForEnv("production") != slog.LevelInfo {
		t.Error("production => info")
	}
}
