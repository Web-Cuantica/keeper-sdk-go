package keeper

import (
	"testing"
)

func TestSamplerForRatio(t *testing.T) {
	casos := []struct {
		ratio    float64
		wantRate float64
		wantDesc string
	}{
		{1.0, 1, "sin muestreo"},
		{2.0, 1, "ratio>1 se trata como sin muestreo"},
		{0.5, 2, "1 de cada 2"},
		{0.1, 10, "1 de cada 10"},
		{0.0, 0, "nunca"},
		{-1, 0, "negativo => nunca"},
	}
	for _, c := range casos {
		s, rate := samplerForRatio(c.ratio)
		if s == nil {
			t.Errorf("%s: sampler nil", c.wantDesc)
		}
		if rate != c.wantRate {
			t.Errorf("%s: sample_rate(%v) = %v, quería %v", c.wantDesc, c.ratio, rate, c.wantRate)
		}
	}
}

func TestResolveSampleRatioOpcionGana(t *testing.T) {
	r := 0.25
	env := func(string) string { return "always_off" } // debe ignorarse
	if got := resolveSampleRatio(&r, env); got != 0.25 {
		t.Errorf("la opción debe ganar al entorno: %v", got)
	}
}

func TestResolveSampleRatioEntorno(t *testing.T) {
	casos := []struct {
		sampler string
		arg     string
		want    float64
	}{
		{"", "", 1},
		{"always_on", "", 1},
		{"always_off", "", 0},
		{"parentbased_always_off", "", 0},
		{"traceidratio", "0.2", 0.2},
		{"parentbased_traceidratio", "0.05", 0.05},
		{"traceidratio", "no-numero", 1}, // arg inválido => sin muestreo
		{"desconocido", "", 1},
		{"traceidratio", "5", 1}, // se acota a [0,1]
	}
	for _, c := range casos {
		env := func(k string) string {
			switch k {
			case "OTEL_TRACES_SAMPLER":
				return c.sampler
			case "OTEL_TRACES_SAMPLER_ARG":
				return c.arg
			}
			return ""
		}
		if got := resolveSampleRatio(nil, env); got != c.want {
			t.Errorf("sampler=%q arg=%q => %v, quería %v", c.sampler, c.arg, got, c.want)
		}
	}
}

func TestClampRatio(t *testing.T) {
	if clampRatio(-0.5) != 0 {
		t.Error("negativo debe acotarse a 0")
	}
	if clampRatio(1.5) != 1 {
		t.Error("mayor a 1 debe acotarse a 1")
	}
	if clampRatio(0.3) != 0.3 {
		t.Error("valor válido no debe cambiar")
	}
}
