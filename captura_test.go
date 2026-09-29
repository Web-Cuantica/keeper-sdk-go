package keeper

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func restaurarGlobales(t *testing.T) {
	t.Helper()
	prevDefault, prevLogger := slog.Default(), logger
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		mu.Lock()
		logger = prevLogger
		mu.Unlock()
		setContrato(nil)
		setRedactKeys(defaultRedactKeys())
		setHashConfig("", defaultHashKeys())
	})
}

// Es la prueba que un servicio necesita para decir "cero datos personales en la telemetría":
// una persona sintética entra por todos lados y no sale por ninguno.
func TestCaptura_UnDatoPersonalNoSalePorNingunLado(t *testing.T) {
	restaurarGlobales(t)
	captura := NuevaCaptura()
	apagar, err := Start(context.Background(),
		WithService("prueba"), WithLevel("debug"), WithHashPepper("pepper-de-prueba"), WithCaptura(captura),
		WithContrato(Contrato{Modo: Descartar, Atributos: map[string]Clasificacion{
			"pld.desenlace":        Operativo,
			"pld.solicitante.curp": Identificador,
		}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = apagar(context.Background()) })
	contratoEnUso().avisoLimit = 0

	const nombre, curp = "Juan Pérez Gómez", "GODE561231HDFRRN09"
	Logger().Info("evaluada", "pld.desenlace", "pendiente", "persona", nombre, "pld.solicitante.curp", curp)
	_, span := otel.Tracer("prueba").Start(context.Background(), "regla lista_ofac")
	span.SetAttributes(attribute.String("pld.desenlace", "aceptado"), attribute.String("nombre_buscado", nombre))
	span.AddEvent("coincidencia", trace.WithAttributes(attribute.String("pld.solicitante.curp", curp)))
	span.End()

	for _, dato := range []string{nombre, curp} {
		if donde := captura.Buscar(dato); len(donde) > 0 {
			t.Errorf("%q salió en: %v", dato, donde)
		}
	}
	if len(captura.Spans()) != 1 || len(captura.Logs()) != 1 {
		t.Fatalf("esperaba un span y un log, hubo %d y %d", len(captura.Spans()), len(captura.Logs()))
	}
	if donde := captura.Buscar("pendiente"); len(donde) != 1 || !strings.HasPrefix(donde[0], "log") {
		t.Errorf("lo declarado debe verse en el log: %v", donde)
	}
	if donde := captura.Buscar("aceptado"); len(donde) != 1 || !strings.HasPrefix(donde[0], "span") {
		t.Errorf("lo declarado debe verse en el span: %v", donde)
	}
	if v := captura.Logs()[0].Atributos["pld.solicitante.curp"]; !strings.HasPrefix(v, hashPrefix) {
		t.Errorf("con pepper el identificador sale como hash, salió %q", v)
	}

	captura.Limpiar()
	if len(captura.Spans())+len(captura.Logs()) != 0 {
		t.Error("Limpiar no vació la captura")
	}
}

func TestCapturaSlog_AplanaGrupos(t *testing.T) {
	captura := NuevaCaptura()
	slog.New(capturaSlog{c: captura}).WithGroup("g").With("a", 1).Info("m", slog.Group("h", "b", 2))
	attrs := captura.Logs()[0].Atributos
	if attrs["g.a"] != "1" || attrs["g.h.b"] != "2" {
		t.Fatalf("los grupos deben aplanarse como prefijo: %v", attrs)
	}
}
