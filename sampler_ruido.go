package keeper

import (
	"strings"

	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// samplerSinRuidoDeSondeo descarta spans RAÍZ cuyo nombre empieza con alguno de
// los prefijos configurados, y deja pasar todo lo demás.
//
// La distinción "raíz" es la clave, y por eso no basta con filtrar por nombre:
//
//   - Un `gorm.Row` SIN padre es un sondeo — el poller del outbox preguntando
//     cada 2 s si hay trabajo. No es una unidad de trabajo, no tiene valor de
//     depuración, y a ese ritmo domina la lista de trazas: el operador abre
//     Keeper y solo ve eso (§7, "ruido de sondeo: health-checks sin muestrear").
//   - Ese MISMO `gorm.Row` colgando de `POST /api/v1/quotation` sí importa:
//     revela N+1 y consultas lentas dentro de una petición real. Se conserva.
//
// Alternativa descartada: apagar la instrumentación de GORM. Tiraría también los
// spans de consulta útiles, que son justo los que explican una latencia alta.
type samplerSinRuidoDeSondeo struct {
	base     sdktrace.Sampler
	prefijos []string
}

func (s samplerSinRuidoDeSondeo) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// Solo se evalúa la raíz: si ya hay un span padre válido, el span pertenece a
	// una unidad de trabajo real y la decisión la toma el sampler base.
	if !trace.SpanContextFromContext(p.ParentContext).IsValid() {
		for _, pre := range s.prefijos {
			if strings.HasPrefix(p.Name, pre) {
				return sdktrace.SamplingResult{Decision: sdktrace.Drop}
			}
		}
	}
	return s.base.ShouldSample(p)
}

func (s samplerSinRuidoDeSondeo) Description() string {
	return "SinRuidoDeSondeo{" + strings.Join(s.prefijos, ",") + "}+" + s.base.Description()
}

// conFiltroDeRuido envuelve el sampler base si hay prefijos que descartar.
func conFiltroDeRuido(base sdktrace.Sampler, prefijos []string) sdktrace.Sampler {
	limpios := make([]string, 0, len(prefijos))
	for _, p := range prefijos {
		if p = strings.TrimSpace(p); p != "" {
			limpios = append(limpios, p)
		}
	}
	if len(limpios) == 0 {
		return base
	}
	return samplerSinRuidoDeSondeo{base: base, prefijos: limpios}
}
