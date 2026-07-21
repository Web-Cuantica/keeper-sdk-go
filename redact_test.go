package keeper

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestMatchRedact(t *testing.T) {
	keys := defaultRedactKeys()
	casos := map[string]bool{
		"password":      true,
		"Password":      true,  // case-insensitive
		"user_password": true,  // subcadena
		"userToken":     true,  // subcadena (token)
		"authorization": true,  // secreto
		"email":         true,  // PII
		"curp":          true,  // PII
		"rfc":           true,  // PII
		"inspection_id": false, // negocio
		"sales_order":   false,
		"rcpt_id":       false,
	}
	for key, want := range casos {
		if got := matchRedact(key, keys); got != want {
			t.Errorf("matchRedact(%q) = %v, quería %v", key, got, want)
		}
	}
}

func TestMatchRedactClavesExtra(t *testing.T) {
	keys := defaultRedactKeys()
	keys["eco_secreto"] = struct{}{}
	if !matchRedact("eco_secreto", keys) {
		t.Errorf("clave extra no censurada")
	}
}

// recordingHandler captura el Record que le llega para inspeccionar los atributos
// ya redactados (incluyendo los de grupos anidados).
type recordingHandler struct{ rec slog.Record }

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.rec = r
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func collectAttrs(r slog.Record) map[string]slog.Value {
	out := map[string]slog.Value{}
	r.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

func TestRedactHandlerTopLevel(t *testing.T) {
	rec := &recordingHandler{}
	h := redactHandler{next: rec, keys: defaultRedactKeys()}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(
		slog.String("password", "hunter2"),
		slog.String("user_password", "x"),
		slog.Int("inspection_id", 97125),
		slog.String("email", "a@b.com"),
	)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	attrs := collectAttrs(rec.rec)
	if attrs["password"].String() != redactCensor {
		t.Errorf("password no censurado: %v", attrs["password"])
	}
	if attrs["user_password"].String() != redactCensor {
		t.Errorf("user_password (subcadena) no censurado: %v", attrs["user_password"])
	}
	if attrs["email"].String() != redactCensor {
		t.Errorf("email (PII) no censurado: %v", attrs["email"])
	}
	if attrs["inspection_id"].Int64() != 97125 {
		t.Errorf("campo de negocio alterado: %v", attrs["inspection_id"])
	}
}

func TestRedactHandlerGrupoAnidado(t *testing.T) {
	rec := &recordingHandler{}
	h := redactHandler{next: rec, keys: defaultRedactKeys()}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(
		slog.Group("user",
			slog.String("name", "jorge"),
			slog.String("password", "hunter2"),
			slog.Group("meta", slog.String("authorization", "Bearer z")),
		),
	)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	attrs := collectAttrs(rec.rec)
	g := attrs["user"]
	if g.Kind() != slog.KindGroup {
		t.Fatalf("user no es grupo: %v", g.Kind())
	}
	inner := map[string]slog.Value{}
	for _, a := range g.Group() {
		inner[a.Key] = a.Value
	}
	if inner["name"].String() != "jorge" {
		t.Errorf("name alterado: %v", inner["name"])
	}
	if inner["password"].String() != redactCensor {
		t.Errorf("password anidado no censurado: %v", inner["password"])
	}
	meta := inner["meta"]
	if meta.Kind() != slog.KindGroup {
		t.Fatalf("meta no es grupo")
	}
	for _, a := range meta.Group() {
		if a.Key == "authorization" && a.Value.String() != redactCensor {
			t.Errorf("authorization (2 niveles) no censurado: %v", a.Value)
		}
	}
}
