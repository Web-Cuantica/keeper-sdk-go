package keeper

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestNormalizeID(t *testing.T) {
	casos := []struct{ in, want string }{
		{"  A@B.COM  ", "a@b.com"},
		{"XEXX010101HNEXXXA4", "xexx010101hnexxxa4"},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range casos {
		if got := normalizeID(c.in); got != c.want {
			t.Errorf("normalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHashIDWithPepperConsistente(t *testing.T) {
	pepper := "org-pepper-secreto"
	a := hashIDWithPepper(pepper, "a@b.com")
	b := hashIDWithPepper(pepper, "  A@B.COM ")
	if a == "" || !IsHashed(a) {
		t.Fatalf("hash inválido: %q", a)
	}
	if a != b {
		t.Errorf("misma identidad normalizada debe dar mismo hash:\n  %s\n  %s", a, b)
	}
	// Distinto pepper ⇒ distinto hash.
	if hashIDWithPepper("otro", "a@b.com") == a {
		t.Error("pepper distinto no debe producir el mismo hash")
	}
	// Distinto valor ⇒ distinto hash.
	if hashIDWithPepper(pepper, "otro@b.com") == a {
		t.Error("valores distintos no deben colisionar")
	}
}

func TestHashIDWithPepperVacios(t *testing.T) {
	if hashIDWithPepper("", "a@b.com") != "" {
		t.Error("sin pepper debe devolver vacío")
	}
	if hashIDWithPepper("p", "") != "" {
		t.Error("valor vacío debe devolver vacío")
	}
}

func TestHashIDUsaPepperGlobal(t *testing.T) {
	setHashConfig("pepper-test", defaultHashKeys())
	defer setHashConfig("", defaultHashKeys())

	got := HashID("curp-demo")
	want := hashIDWithPepper("pepper-test", "curp-demo")
	if got != want {
		t.Errorf("HashID = %q, want %q", got, want)
	}
	setHashConfig("", defaultHashKeys())
	if HashID("curp-demo") != "" {
		t.Error("sin pepper global HashID debe ser vacío")
	}
}

func TestIsHashed(t *testing.T) {
	if !IsHashed("h1:abcdef") {
		t.Error("debió reconocer prefijo h1:")
	}
	if IsHashed("***") || IsHashed("h1:") || IsHashed("sha256:x") {
		t.Error("falsos positivos en IsHashed")
	}
}

func TestRedactHandlerHasheaPIIConPepper(t *testing.T) {
	rec := &recordingHandler{}
	pepper := "p-org"
	h := redactHandler{
		next:     rec,
		keys:     defaultRedactKeys(),
		hashKeys: defaultHashKeys(),
		pepper:   pepper,
	}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(
		slog.String("email", "a@b.com"),
		slog.String("curp", "XEXX010101HNEXXXA4"),
		slog.String("password", "hunter2"),
		slog.Int("usr_id", 4471),
	)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	attrs := collectAttrs(rec.rec)

	wantEmail := hashIDWithPepper(pepper, "a@b.com")
	if attrs["email"].String() != wantEmail {
		t.Errorf("email: got %q want %q", attrs["email"].String(), wantEmail)
	}
	if !IsHashed(attrs["curp"].String()) {
		t.Errorf("curp no hasheado: %v", attrs["curp"])
	}
	if attrs["password"].String() != redactCensor {
		t.Errorf("password NUNCA se hashea: %v", attrs["password"])
	}
	if attrs["usr_id"].Int64() != 4471 {
		t.Errorf("negocio alterado: %v", attrs["usr_id"])
	}
}

func TestRedactHandlerSinPepperCensuraPII(t *testing.T) {
	rec := &recordingHandler{}
	h := redactHandler{next: rec, keys: defaultRedactKeys(), hashKeys: defaultHashKeys(), pepper: ""}
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(slog.String("email", "a@b.com"))
	_ = h.Handle(context.Background(), r)
	if collectAttrs(rec.rec)["email"].String() != redactCensor {
		t.Error("sin pepper, email debe censurarse con ***")
	}
}

func TestRedactHandlerHasheaGrupoAnidado(t *testing.T) {
	rec := &recordingHandler{}
	pepper := "p"
	h := redactHandler{next: rec, keys: defaultRedactKeys(), hashKeys: defaultHashKeys(), pepper: pepper}
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(slog.Group("user",
		slog.String("email", "a@b.com"),
		slog.String("token", "secreto"),
	))
	_ = h.Handle(context.Background(), r)

	g := collectAttrs(rec.rec)["user"]
	if g.Kind() != slog.KindGroup {
		t.Fatal("user no es grupo")
	}
	inner := map[string]slog.Value{}
	for _, a := range g.Group() {
		inner[a.Key] = a.Value
	}
	if inner["email"].String() != hashIDWithPepper(pepper, "a@b.com") {
		t.Errorf("email anidado no hasheado: %v", inner["email"])
	}
	if inner["token"].String() != redactCensor {
		t.Errorf("token anidado no censurado: %v", inner["token"])
	}
}

func TestRedactAttrsConPepper(t *testing.T) {
	setRedactKeys(defaultRedactKeys())
	setHashConfig("pepper-attrs", defaultHashKeys())
	defer setHashConfig("", defaultHashKeys())

	out := RedactAttrs([]slog.Attr{
		slog.String("rfc", "XAXX010101000"),
		slog.String("authorization", "Bearer x"),
	})
	got := map[string]string{}
	for _, a := range out {
		got[a.Key] = a.Value.String()
	}
	if !IsHashed(got["rfc"]) {
		t.Errorf("rfc no hasheado: %q", got["rfc"])
	}
	if got["authorization"] != redactCensor {
		t.Errorf("authorization debe censurarse: %q", got["authorization"])
	}
}

func TestWithHashPepperYKeysEnConfig(t *testing.T) {
	cfg := resolveConfig(WithHashPepper("desde-opcion"), WithHashKeys("email", "negocio_id"))
	if cfg.hashPepper != "desde-opcion" {
		t.Errorf("pepper = %q", cfg.hashPepper)
	}
	if _, ok := cfg.hashKeys["email"]; !ok {
		t.Error("falta email en hashKeys")
	}
	if _, ok := cfg.hashKeys["negocio_id"]; !ok {
		t.Error("falta negocio_id en hashKeys")
	}
	if _, ok := cfg.hashKeys["curp"]; ok {
		t.Error("WithHashKeys reemplaza el default; curp no debería estar")
	}
}

func TestDefaultHashKeysNoIncluyeSecretos(t *testing.T) {
	keys := defaultHashKeys()
	for _, secret := range []string{"password", "token", "authorization", "cookie"} {
		if _, ok := keys[secret]; ok {
			t.Errorf("%s no debe ser hasheable", secret)
		}
	}
	for _, id := range []string{"email", "curp", "rfc", "vin", "ssn"} {
		if _, ok := keys[id]; !ok {
			t.Errorf("falta identificador %s en defaultHashKeys", id)
		}
	}
}
