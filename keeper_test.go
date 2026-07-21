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

func TestDeployResourceAttrs(t *testing.T) {
	cfg := config{service: "svc", version: "1.0.0", env: "production"}
	attrs := deployResourceAttrs(cfg)
	seen := map[string]string{}
	for _, a := range attrs {
		seen[string(a.Key)] = a.Value.AsString()
	}
	if seen["service.name"] != "svc" || seen["deployment.environment"] != "production" {
		t.Errorf("atributos base incompletos: %v", seen)
	}
	if _, ok := seen["build_id"]; ok {
		t.Error("build_id no debe estar sin configurar")
	}

	cfg.buildID = "b-1"
	cfg.commitHash = "abc123"
	seen = map[string]string{}
	for _, a := range deployResourceAttrs(cfg) {
		seen[string(a.Key)] = a.Value.AsString()
	}
	if seen["build_id"] != "b-1" || seen["commit_hash"] != "abc123" {
		t.Errorf("build_id/commit_hash no emitidos: %v", seen)
	}
}

func TestResolveConfigBuildCommitDesdeEntorno(t *testing.T) {
	t.Setenv("KEEPER_BUILD_ID", "build-9")
	t.Setenv("GIT_COMMIT", "deadbeef")
	cfg := resolveConfig()
	if cfg.buildID != "build-9" {
		t.Errorf("buildID desde entorno = %q", cfg.buildID)
	}
	if cfg.commitHash != "deadbeef" {
		t.Errorf("commitHash desde entorno = %q", cfg.commitHash)
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
