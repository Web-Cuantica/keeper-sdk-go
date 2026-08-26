package keeper

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func ctxConPadre() context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

// El poller del outbox consulta la base cada 2 s y cada vuelta nace un span raíz.
// A ese ritmo es lo único que se ve en la lista de trazas.
func TestFiltroDeRuido_DescartaElSondeoRaiz(t *testing.T) {
	s := conFiltroDeRuido(sdktrace.AlwaysSample(), []string{"gorm."})

	got := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(), // sin padre = raíz
		Name:          "gorm.Row",
	})

	if got.Decision != sdktrace.Drop {
		t.Errorf("un gorm.Row sin padre es sondeo y debía descartarse, se obtuvo %v", got.Decision)
	}
}

// La misma consulta DENTRO de una petición real sí importa: revela N+1 y
// consultas lentas. Filtrar por nombre a secas la habría tirado también.
func TestFiltroDeRuido_ConservaLaConsultaDentroDeUnRequest(t *testing.T) {
	s := conFiltroDeRuido(sdktrace.AlwaysSample(), []string{"gorm."})

	got := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctxConPadre(),
		Name:          "gorm.Row",
	})

	if got.Decision == sdktrace.Drop {
		t.Error("un gorm.Row bajo un request explica latencia: no se descarta")
	}
}

// Un span raíz legítimo (una petición HTTP, un handler del outbox) pasa.
func TestFiltroDeRuido_NoTocaLoQueNoCoincide(t *testing.T) {
	s := conFiltroDeRuido(sdktrace.AlwaysSample(), []string{"gorm."})

	for _, nombre := range []string{"POST /api/v1/quotation", "process quote.sent", "cotizacion.enviar-correo"} {
		got := s.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: context.Background(),
			Name:          nombre,
		})
		if got.Decision == sdktrace.Drop {
			t.Errorf("%q es una unidad de trabajo real y no debía descartarse", nombre)
		}
	}
}

// Sin prefijos configurados el sampler base queda intacto: la función es opt-in.
func TestFiltroDeRuido_SinPrefijosDevuelveElBase(t *testing.T) {
	base := sdktrace.AlwaysSample()
	if got := conFiltroDeRuido(base, nil); got != base {
		t.Error("sin prefijos debe devolverse el sampler base tal cual")
	}
	if got := conFiltroDeRuido(base, []string{"", "   "}); got != base {
		t.Error("prefijos vacíos no cuentan")
	}
}

// El filtro decide ANTES que el muestreo: no tiene sentido gastar cuota en spans
// que nadie va a leer.
func TestFiltroDeRuido_RespetaLaDecisionDelBase(t *testing.T) {
	s := conFiltroDeRuido(sdktrace.NeverSample(), []string{"gorm."})

	got := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Name:          "POST /api/v1/quotation",
	})
	if got.Decision != sdktrace.Drop {
		t.Error("lo que no coincide sigue la decisión del sampler base")
	}
}
