package keeper

import (
	"context"
	"testing"
)

func TestParseClientDesktop(t *testing.T) {
	c := ParseClient("189.217.103.61",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	if c.Address != "189.217.103.61" {
		t.Errorf("address = %q", c.Address)
	}
	if c.Browser != "Chrome" {
		t.Errorf("browser = %q, want Chrome", c.Browser)
	}
	if c.OS != "Windows" {
		t.Errorf("os = %q, want Windows", c.OS)
	}
	if c.DeviceType != "desktop" {
		t.Errorf("device = %q, want desktop", c.DeviceType)
	}
	if c.UserAgent == "" {
		t.Error("UserAgent no debería quedar vacío")
	}
}

func TestParseClientMobile(t *testing.T) {
	c := ParseClient("10.0.0.1",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	if c.DeviceType != "mobile" {
		t.Errorf("device = %q, want mobile", c.DeviceType)
	}
	if c.OS == "" {
		t.Error("OS no debería quedar vacío en un iPhone")
	}
}

func TestParseClientEmptyUA(t *testing.T) {
	c := ParseClient("1.2.3.4", "")
	if c.Address != "1.2.3.4" || c.Browser != "" || c.OS != "" || c.DeviceType != "" {
		t.Errorf("UA vacío debe dejar solo la dirección: %+v", c)
	}
}

func TestClientContextRoundTrip(t *testing.T) {
	ctx := ContextWithClient(context.Background(), Client{Address: "1.1.1.1", Browser: "Firefox"})
	got, ok := ClientFromContext(ctx)
	if !ok || got.Address != "1.1.1.1" || got.Browser != "Firefox" {
		t.Fatalf("round-trip fallido: %+v ok=%v", got, ok)
	}
	if _, ok := ClientFromContext(context.Background()); ok {
		t.Fatal("un contexto sin client no debe reportar ok=true")
	}
}
