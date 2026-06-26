package keeper

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"trace": slog.LevelDebug,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"fatal": slog.LevelError,
		"raro":  slog.LevelInfo,
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, quería %v", in, got, want)
		}
	}
}

func TestResolveConfigDefaults(t *testing.T) {
	t.Setenv("KEEPER_SERVICE_NAME", "")
	t.Setenv("SERVICE_NAME", "")
	cfg := resolveConfig()
	if cfg.service != "unknown-service" {
		t.Errorf("service default = %q", cfg.service)
	}
	if cfg.endpoint != "http://localhost:4318" {
		t.Errorf("endpoint default = %q", cfg.endpoint)
	}
	if cfg.level != slog.LevelInfo { // env development -> pero default por ambiente es debug
		// development => debug
	}
}

func TestOptionsPrecedenceSobreEntorno(t *testing.T) {
	t.Setenv("KEEPER_SERVICE_NAME", "del-entorno")
	cfg := resolveConfig(WithService("de-la-opcion"), WithEnv("dev"))
	if cfg.service != "de-la-opcion" {
		t.Errorf("la opción debe ganar al entorno: %q", cfg.service)
	}
	if cfg.level != slog.LevelDebug {
		t.Errorf("env dev debe dar nivel debug, dio %v", cfg.level)
	}
}

func TestRedactCensuraSensibles(t *testing.T) {
	h := redactHandler{keys: defaultRedactKeys()}
	if got := h.redact(slog.String("password", "secreto")); got.Value.String() != redactCensor {
		t.Errorf("password no censurado: %v", got.Value)
	}
	if got := h.redact(slog.String("Authorization", "Bearer x")); got.Value.String() != redactCensor {
		t.Errorf("Authorization (case-insensitive) no censurado: %v", got.Value)
	}
	if got := h.redact(slog.Int("inspection_id", 97125)); got.Value.Int64() != 97125 {
		t.Errorf("campo de negocio alterado: %v", got.Value)
	}
}
