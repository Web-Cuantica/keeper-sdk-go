package keeper

import "testing"

func TestSafeUTF8(t *testing.T) {
	casos := []struct {
		nombre string
		in     string
		want   string
	}{
		{"ascii", "ok", "ok"},
		{"ruta válida", "/api/v1/fleet", "/api/v1/fleet"},
		{"utf8 válido multibyte", "café ñandú", "café ñandú"},
		{"vacío", "", ""},
		{"bytes inválidos", "\xff\xfe", ""},
		{"byte de continuación suelto", "a\x80b", "ab"},
		{"ruta de bot con basura", "/.env\xc3\x28", "/.env("}, // \xc3 (líder incompleto) se elimina; ( es ASCII válido y se conserva
	}
	for _, c := range casos {
		if got := SafeUTF8(c.in); got != c.want {
			t.Errorf("%s: SafeUTF8(%q) = %q, want %q", c.nombre, c.in, got, c.want)
		}
	}
}
