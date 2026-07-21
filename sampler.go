package keeper

import (
	"math"
	"strconv"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Modelo de muestreo (OBSERVABILITY-ENGINEERING.md §7):
//   - Muestreo de cabeza por proporción de trace_id: ParentBased(TraceIDRatioBased(ratio)).
//   - sample_rate = cuántos eventos representa cada evento conservado = round(1/ratio).
//     Con ratio = 1 (sin muestreo) sample_rate = 1. Este valor se estampa en el span raíz
//     (ver keeperfiber) para poder reponderar en el análisis.
//   - Los logs NO se muestrean por el sampler de trazas; se correlacionan por trace_id.

// samplerForRatio devuelve el sampler y el sample_rate correspondiente a una proporción.
// ratio se acota a [0,1]. ratio<=0 => nunca muestrear; ratio>=1 => siempre.
func samplerForRatio(ratio float64) (sdktrace.Sampler, float64) {
	switch {
	case ratio <= 0:
		return sdktrace.NeverSample(), 0
	case ratio >= 1:
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), 1
	default:
		rate := math.Round(1 / ratio)
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), rate
	}
}

// resolveSampleRatio decide la proporción de muestreo con precedencia:
// opción (optRatio no-nil) > OTEL_TRACES_SAMPLER(+_ARG) del entorno > 1.0 (sin muestreo).
// getenv se inyecta para poder testear sin tocar el entorno real.
func resolveSampleRatio(optRatio *float64, getenv func(string) string) float64 {
	if optRatio != nil {
		return clampRatio(*optRatio)
	}
	sampler := strings.ToLower(strings.TrimSpace(getenv("OTEL_TRACES_SAMPLER")))
	arg := strings.TrimSpace(getenv("OTEL_TRACES_SAMPLER_ARG"))
	switch sampler {
	case "always_off", "parentbased_always_off":
		return 0
	case "always_on", "parentbased_always_on", "":
		if sampler == "" {
			return 1
		}
		return 1
	case "traceidratio", "parentbased_traceidratio":
		if r, err := strconv.ParseFloat(arg, 64); err == nil {
			return clampRatio(r)
		}
		return 1
	default:
		return 1
	}
}

func clampRatio(r float64) float64 {
	if r < 0 || math.IsNaN(r) {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}
