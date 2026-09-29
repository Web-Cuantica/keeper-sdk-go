package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func contratoDePrueba(t *testing.T, modo ModoContrato) *contratoActivo {
	t.Helper()
	c := nuevoContratoActivo(Contrato{Modo: modo, Atributos: map[string]Clasificacion{
		"pld.desenlace":        Operativo,
		"pld.solicitante.curp": Identificador,
	}})
	c.avisoLimit = 0 // sin ruido en stderr durante las pruebas
	setContrato(c)
	t.Cleanup(func() { setContrato(nil) })
	return c
}

// capturaHandler guarda cada registro con los atributos fijados por WithAttrs.
type capturaHandler struct {
	fijos     []slog.Attr
	registros *[]slog.Record
}

func nuevaCaptura() *capturaHandler { return &capturaHandler{registros: &[]slog.Record{}} }

func (h *capturaHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturaHandler) Handle(_ context.Context, r slog.Record) error {
	nr := r.Clone()
	nr.AddAttrs(h.fijos...)
	*h.registros = append(*h.registros, nr)
	return nil
}
func (h *capturaHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &capturaHandler{fijos: append(append([]slog.Attr{}, h.fijos...), a...), registros: h.registros}
}
func (h *capturaHandler) WithGroup(string) slog.Handler { return h }
func (h *capturaHandler) ultimo(t *testing.T) map[string]slog.Value {
	t.Helper()
	if len(*h.registros) == 0 {
		t.Fatal("no se capturó ningún log")
	}
	return collectAttrs((*h.registros)[len(*h.registros)-1])
}

func violacion(vs []Violacion, clave, senal string) (Violacion, bool) {
	for _, v := range vs {
		if v.Clave == clave && v.Senal == senal {
			return v, true
		}
	}
	return Violacion{}, false
}

// Un nombre de persona en un campo mal elegido es exactamente la fuga que el
// contrato existe para impedir: la censura por nombre de campo no la ve.
func TestContratoLogs_DescartarQuitaLoNoDeclaradoYLoCuenta(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	capt := nuevaCaptura()
	slog.New(contratoHandler{next: capt, c: c}).Info("evaluada",
		"pld.desenlace", "pendiente", "persona", "Juan Pérez", "request_id", "r1")

	attrs := capt.ultimo(t)
	if _, ok := attrs["persona"]; ok {
		t.Fatal("un atributo no declarado salió en modo Descartar")
	}
	if attrs["pld.desenlace"].String() != "pendiente" || attrs["request_id"].String() != "r1" {
		t.Fatalf("se perdió un atributo declarado o propio del SDK: %v", attrs)
	}
	v, ok := violacion(Violaciones(), "persona", senalLogs)
	if !ok || v.Descartes != 1 || v.Reportes != 0 {
		t.Fatalf("la violación no quedó contada como descarte: %+v", Violaciones())
	}
}

// Reportar existe para adoptar el contrato en un servicio que ya emite de todo:
// nada se pierde, pero todo lo no declarado queda a la vista.
func TestContratoLogs_ReportarDejaPasarYLoCuenta(t *testing.T) {
	c := contratoDePrueba(t, Reportar)
	capt := nuevaCaptura()
	slog.New(contratoHandler{next: capt, c: c}).Info("evaluada", "llamante", "core-credito")

	if capt.ultimo(t)["llamante"].String() != "core-credito" {
		t.Fatal("en modo Reportar el atributo debía salir")
	}
	if v, ok := violacion(Violaciones(), "llamante", senalLogs); !ok || v.Reportes != 1 {
		t.Fatalf("la violación no quedó contada como reporte: %+v", Violaciones())
	}
}

func TestContratoLogs_TambienFiltraLoFijadoConWith(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	capt := nuevaCaptura()
	slog.New(contratoHandler{next: capt, c: c}).With("persona", "Juan").Info("hola")

	if _, ok := capt.ultimo(t)["persona"]; ok {
		t.Fatal("logger.With dejó pasar un atributo no declarado")
	}
	if _, ok := violacion(Violaciones(), "persona", senalLogs); !ok {
		t.Fatal("falta la violación de un atributo fijado con With")
	}
}

func TestContrato_UnaEntradaPorClaveConSuConteo(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	c.admite("persona", senalLogs)
	c.admite("persona", senalLogs)
	c.admite("persona", senalTrazas)

	vs := Violaciones()
	if len(vs) != 2 {
		t.Fatalf("esperaba una entrada por clave y señal, hubo %d: %+v", len(vs), vs)
	}
	if v, _ := violacion(vs, "persona", senalLogs); v.Descartes != 2 {
		t.Fatalf("conteo equivocado: %+v", v)
	}
	ReiniciarViolaciones()
	if len(Violaciones()) != 0 {
		t.Fatal("ReiniciarViolaciones no limpió la contabilidad")
	}
}

func TestSinContrato_NoHayViolaciones(t *testing.T) {
	setContrato(nil)
	if Violaciones() != nil {
		t.Fatal("sin contrato no debe haber contabilidad")
	}
	ReiniciarViolaciones() // no debe entrar en pánico
}

func exportarSpan(t *testing.T, c *contratoActivo, attrs []attribute.KeyValue, evento []attribute.KeyValue) tracetest.SpanStub {
	t.Helper()
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exportadorConContrato{next: mem, c: c}))
	_, span := tp.Tracer("prueba").Start(context.Background(), "POST /api/v1/applications")
	span.SetAttributes(attrs...)
	if evento != nil {
		span.AddEvent("regla", trace.WithAttributes(evento...))
	}
	span.End()
	spans := mem.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("esperaba un span exportado, hubo %d", len(spans))
	}
	return spans[0]
}

func mapa(kvs []attribute.KeyValue) map[string]string {
	out := map[string]string{}
	for _, kv := range kvs {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

// Los spans no pasan por la cadena de logs, y las librerías de instrumentación
// ponen atributos por su cuenta: el filtro tiene que vivir en el exportador.
func TestContratoTrazas_FiltraAtributosYEventos(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	s := exportarSpan(t, c,
		[]attribute.KeyValue{attribute.String("pld.desenlace", "pendiente"), attribute.String("persona", "Juan"), attribute.String("http.route", "/x")},
		[]attribute.KeyValue{attribute.String("persona", "Juan"), attribute.String("pld.desenlace", "aceptado")})

	attrs := mapa(s.Attributes)
	if _, ok := attrs["persona"]; ok {
		t.Fatal("el span exportó un atributo no declarado")
	}
	if attrs["pld.desenlace"] != "pendiente" || attrs["http.route"] != "/x" {
		t.Fatalf("se perdió un atributo permitido: %v", attrs)
	}
	if len(s.Events) != 1 || s.Events[0].Name != "regla" {
		t.Fatalf("se perdió el evento: %+v", s.Events)
	}
	if ev := mapa(s.Events[0].Attributes); ev["persona"] != "" || ev["pld.desenlace"] != "aceptado" {
		t.Fatalf("los atributos del evento no se filtraron bien: %v", ev)
	}
	if s.Name != "POST /api/v1/applications" {
		t.Fatalf("el filtro alteró el resto del span: %q", s.Name)
	}
	if _, ok := violacion(Violaciones(), "persona", senalTrazas); !ok {
		t.Fatal("falta la violación en trazas")
	}
}

func TestContratoTrazas_IdentificadorNuncaSaleEnClaro(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	t.Cleanup(func() { setHashConfig("", defaultHashKeys()) })

	setHashConfig("pepper-de-prueba", defaultHashKeys())
	conPepper := mapa(exportarSpan(t, c, []attribute.KeyValue{attribute.String("pld.solicitante.curp", "GODE561231HDFRRN09")}, nil).Attributes)
	if v := conPepper["pld.solicitante.curp"]; !strings.HasPrefix(v, hashPrefix) {
		t.Fatalf("con pepper el identificador debía salir como hash, salió %q", v)
	}

	setHashConfig("", defaultHashKeys())
	sinPepper := mapa(exportarSpan(t, c, []attribute.KeyValue{attribute.String("pld.solicitante.curp", "GODE561231HDFRRN09")}, nil).Attributes)
	if v := sinPepper["pld.solicitante.curp"]; v != redactCensor {
		t.Fatalf("sin pepper el identificador debía salir censurado, salió %q", v)
	}
	if len(Violaciones()) != 0 {
		t.Fatalf("un identificador declarado no es una violación: %+v", Violaciones())
	}
}

func TestContratoTrazas_SinCambiosConservaLosAtributos(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	attrs := mapa(exportarSpan(t, c, []attribute.KeyValue{attribute.String("pld.desenlace", "aceptado"), attribute.Int("http.response.status_code", 201)}, nil).Attributes)
	if attrs["pld.desenlace"] != "aceptado" || attrs["http.response.status_code"] != "201" || len(attrs) != 2 {
		t.Fatalf("un span que cumple el contrato debe salir intacto: %v", attrs)
	}
}

func recolectar(t *testing.T, r *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

func buscarSuma(rm metricdata.ResourceMetrics, nombre string) (metricdata.Sum[int64], bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == nombre {
				s, ok := m.Data.(metricdata.Sum[int64])
				return s, ok
			}
		}
	}
	return metricdata.Sum[int64]{}, false
}

// En métricas un identificador es además cardinalidad sin techo: se quita siempre.
func TestContratoMetricas_QuitaNoDeclaradosEIdentificadores(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	lector := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(lector), sdkmetric.WithView(vistaConContrato(c)))
	contador, err := mp.Meter("prueba").Int64Counter("pld.evaluaciones")
	if err != nil {
		t.Fatal(err)
	}
	contador.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String("pld.desenlace", "pendiente"),
		attribute.String("persona", "Juan"),
		attribute.String("pld.solicitante.curp", "GODE561231HDFRRN09")))

	suma, ok := buscarSuma(recolectar(t, lector), "pld.evaluaciones")
	if !ok || len(suma.DataPoints) != 1 {
		t.Fatalf("no se recolectó la métrica: %+v", suma)
	}
	attrs := mapa(suma.DataPoints[0].Attributes.ToSlice())
	if len(attrs) != 1 || attrs["pld.desenlace"] != "pendiente" {
		t.Fatalf("la métrica debía quedar solo con el atributo declarado: %v", attrs)
	}
	if _, ok := violacion(Violaciones(), "persona", senalMetricas); !ok {
		t.Fatal("falta la violación en métricas")
	}
	if _, ok := violacion(Violaciones(), "pld.solicitante.curp", senalMetricas); ok {
		t.Fatal("un identificador declarado no debe contarse como violación")
	}
}

func TestContrato_MetricaDeViolacionesSinLaClave(t *testing.T) {
	c := contratoDePrueba(t, Descartar)
	lector := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(lector))
	if err := registrarMetricaDeViolaciones(mp.Meter("prueba"), c); err != nil {
		t.Fatal(err)
	}
	c.admite("persona", senalLogs)
	c.admite("domicilio", senalLogs)

	suma, ok := buscarSuma(recolectar(t, lector), "keeper.contrato.violaciones")
	if !ok || len(suma.DataPoints) != 1 {
		t.Fatalf("esperaba un punto por señal y acción: %+v", suma)
	}
	p := suma.DataPoints[0]
	attrs := mapa(p.Attributes.ToSlice())
	if p.Value != 2 || attrs["keeper.contrato.senal"] != senalLogs || attrs["keeper.contrato.accion"] != "descartado" || len(attrs) != 2 {
		t.Fatalf("punto inesperado (la clave no debe viajar en la métrica): valor=%d attrs=%v", p.Value, attrs)
	}
}

// Prueba de punta a punta del cableado de Start: es lo que usa la prueba de
// contrato de un servicio.
func TestStart_ConContratoSinExportar(t *testing.T) {
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

	shutdown, err := Start(context.Background(),
		WithService("prueba"), WithSinExportar(), WithLevel("debug"),
		WithContrato(Contrato{Modo: Descartar, Atributos: map[string]Clasificacion{
			"pld.desenlace":          Operativo,
			"pld.solicitante.huella": Identificador,
		}}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })
	contratoEnUso().avisoLimit = 0
	ReiniciarViolaciones()

	Logger().Info("evaluada", "pld.desenlace", "pendiente", "persona", "Juan")
	_, span := otel.Tracer("prueba").Start(context.Background(), "op")
	span.SetAttributes(attribute.String("otra", "x"))
	span.End()

	vs := Violaciones()
	if _, ok := violacion(vs, "persona", senalLogs); !ok {
		t.Fatalf("Start no aplicó el contrato a los logs: %+v", vs)
	}
	if _, ok := violacion(vs, "otra", senalTrazas); !ok {
		t.Fatalf("Start no aplicó el contrato a las trazas: %+v", vs)
	}
	if !matchRedact("pld.solicitante.huella", getHashKeys()) {
		t.Fatal("un identificador del contrato debe sumarse a las claves de hash")
	}
}

// Con WithLogStdout los logs siguen llegando a `docker logs`, y con los ids de la
// traza para ir de ahí a Keeper.
func TestDobleSalida_JSONConIdsDeTraza(t *testing.T) {
	var buf bytes.Buffer
	capt := nuevaCaptura()
	log := slog.New(nuevaDobleSalida(capt, &buf))

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("prueba").Start(context.Background(), "op")
	log.With("pld.canal", "alta_api").InfoContext(ctx, "evaluada", "pld.desenlace", "pendiente")
	span.End()

	var linea map[string]any
	if err := json.Unmarshal(buf.Bytes(), &linea); err != nil {
		t.Fatalf("la salida local no es JSON: %v (%q)", err, buf.String())
	}
	if linea["msg"] != "evaluada" || linea["pld.desenlace"] != "pendiente" || linea["pld.canal"] != "alta_api" {
		t.Fatalf("faltan datos en la salida local: %v", linea)
	}
	if linea["trace_id"] != span.SpanContext().TraceID().String() || linea["span_id"] != span.SpanContext().SpanID().String() {
		t.Fatalf("la salida local debe llevar los ids de la traza: %v", linea)
	}
	if _, ok := capt.ultimo(t)["trace_id"]; ok {
		t.Fatal("Keeper ya correlaciona de forma nativa: trace_id no debe duplicarse como atributo")
	}
}

func TestEnvVerdadero(t *testing.T) {
	for _, v := range []string{"true", "1", "Sí", " on "} {
		if !envVerdadero(v) {
			t.Errorf("%q debía ser verdadero", v)
		}
	}
	for _, v := range []string{"", "false", "0", "no"} {
		if envVerdadero(v) {
			t.Errorf("%q debía ser falso", v)
		}
	}
}
