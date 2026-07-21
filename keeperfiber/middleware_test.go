package keeperfiber

import (
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	keeper "github.com/Web-Cuantica/keeper-sdk-go"
)

func TestMiddlewareGeneraRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(Middleware())
	app.Get("/echo", func(c *fiber.Ctx) error { return c.SendString("ok") })

	resp, err := app.Test(httptest.NewRequest("GET", "/echo", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get(RequestIDHeader) == "" {
		t.Error("falta el header X-Request-ID en la respuesta")
	}
}

func TestMiddlewareReutilizaRequestIDEntrante(t *testing.T) {
	app := fiber.New()
	app.Use(Middleware())
	app.Get("/echo", func(c *fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("GET", "/echo", nil)
	req.Header.Set(RequestIDHeader, "rid-fijo-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get(RequestIDHeader); got != "rid-fijo-123" {
		t.Errorf("request_id no reutilizado: %q", got)
	}
}

func TestShouldIgnorePath(t *testing.T) {
	ignore := defaultIgnorePaths
	casos := []struct {
		path string
		want bool
	}{
		{"/health", true},
		{"/api/v1/health", true},
		{"/dyinspectionws/ready", true},
		{"/ping", true},
		{"/metrics", true},
		{"/unhealthy", false}, // no falso positivo
		{"/echo", false},
		{"/recibo", false},
	}
	for _, c := range casos {
		if got := shouldIgnorePath(c.path, ignore); got != c.want {
			t.Errorf("shouldIgnorePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMiddlewareIgnoraHealthSinSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	app := fiber.New()
	app.Use(Middleware())
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/echo", func(c *fiber.Ctx) error { return c.SendString("ok") })

	if _, err := app.Test(httptest.NewRequest("GET", "/health", nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Test(httptest.NewRequest("GET", "/echo", nil)); err != nil {
		t.Fatal(err)
	}

	for _, s := range sr.Ended() {
		if s.Name() == "GET /health" {
			t.Fatalf("health no debe generar span, encontró: %s", s.Name())
		}
	}
	foundEcho := false
	for _, s := range sr.Ended() {
		if s.Name() == "GET /echo" {
			foundEcho = true
		}
	}
	if !foundEcho {
		t.Fatal("echo sí debe generar span")
	}
}

func TestMiddlewareIgnorePathsVacioNoIgnora(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	app := fiber.New()
	app.Use(Middleware(MiddlewareConfig{IgnorePaths: []string{}}))
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })

	if _, err := app.Test(httptest.NewRequest("GET", "/health", nil)); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range sr.Ended() {
		if s.Name() == "GET /health" {
			found = true
		}
	}
	if !found {
		t.Fatal("con IgnorePaths vacío, /health debe instrumentarse")
	}
}

func TestLevelFor(t *testing.T) {
	casos := []struct {
		status     int
		logSuccess bool
		want       slog.Level
	}{
		{200, false, slog.LevelDebug}, // éxito → debug por default (el span es el evento canónico)
		{201, false, slog.LevelDebug},
		{204, false, slog.LevelDebug},
		{304, false, slog.LevelDebug},
		{200, true, slog.LevelInfo}, // con logSuccess el éxito sube a Info (visible en prod)
		{204, true, slog.LevelInfo},
		{304, true, slog.LevelInfo},
		{400, false, slog.LevelWarn}, // 4xx (salvo 404) → warn
		{401, false, slog.LevelWarn},
		{403, false, slog.LevelWarn},
		{409, false, slog.LevelWarn},
		{404, false, slog.LevelDebug}, // 404 → debug: cliente/escáneres de internet, no se exporta
		{404, true, slog.LevelDebug},  // logSuccess NO rescata al 404 (sigue siendo ruido de bots)
		{500, false, slog.LevelError}, // 5xx → error
		{503, false, slog.LevelError},
		{500, true, slog.LevelError},
	}
	for _, c := range casos {
		if got := levelFor(c.status, c.logSuccess); got != c.want {
			t.Errorf("levelFor(%d, %v) = %v, want %v", c.status, c.logSuccess, got, c.want)
		}
	}
}

// TestMensajeDeCierre verifica que el log de cierre lleva método, ruta y una razón
// específica por status en el cuerpo (no el genérico "request completed").
func TestMensajeDeCierre(t *testing.T) {
	casos := []struct {
		method string
		ruta   string
		status int
		want   string
	}{
		{"GET", "/api/v1/fleet/:id", 404, "GET /api/v1/fleet/:id → 404 no encontrado"},
		{"GET", "/api/v1/driving-score/me", 403, "GET /api/v1/driving-score/me → 403 acceso denegado"},
		{"POST", "/api/v1/auth/login", 401, "POST /api/v1/auth/login → 401 no autenticado"},
		{"POST", "/api/v1/clients/", 400, "POST /api/v1/clients/ → 400 solicitud inválida"},
		{"GET", "/api/v1/fleet", 429, "GET /api/v1/fleet → 429 límite de peticiones excedido"},
		{"GET", "/api/v1/fleet", 200, "GET /api/v1/fleet → 200 ok"},
		{"GET", "/api/v1/fleet", 500, "GET /api/v1/fleet → 500 error del servidor"},
		{"GET", "/api/v1/fleet", 418, "GET /api/v1/fleet → 418 error del cliente"},
	}
	for _, c := range casos {
		if got := mensajeDeCierre(c.method, c.ruta, c.status); got != c.want {
			t.Errorf("mensajeDeCierre(%s, %s, %d) = %q, want %q", c.method, c.ruta, c.status, got, c.want)
		}
	}
}

// TestMiddlewareVuelcaEventoCanonico verifica que los atributos de negocio anotados con
// keeper.Annotate durante el handler terminan en el span del request (evento ancho
// canónico), incluido el sample_rate, y que un secreto anotado se redacta.
func TestMiddlewareVuelcaEventoCanonico(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	app := fiber.New()
	app.Use(Middleware())
	app.Get("/recibo", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		keeper.Annotate(ctx,
			slog.Int("rcpt_id", 87772),
			slog.String("sales_order", "1345678"),
			slog.String("password", "hunter2"),
		)
		return c.SendString("ok")
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/recibo", nil)); err != nil {
		t.Fatal(err)
	}

	var span sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == "GET /recibo" {
			span = s
		}
	}
	if span == nil {
		t.Fatal("no se encontró el span del request")
	}
	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value
	}
	if attrs["rcpt_id"].AsInt64() != 87772 {
		t.Errorf("rcpt_id no llegó al span: %v", attrs["rcpt_id"])
	}
	if attrs["sales_order"].AsString() != "1345678" {
		t.Errorf("sales_order no llegó al span: %v", attrs["sales_order"])
	}
	if attrs["password"].AsString() != "***" {
		t.Errorf("password debió redactarse en el span, fue: %v", attrs["password"])
	}
	if _, ok := attrs["sample_rate"]; !ok {
		t.Errorf("falta sample_rate en el span")
	}
}

func TestMiddlewareRecuperaPanic(t *testing.T) {
	app := fiber.New()
	app.Use(Middleware())
	app.Get("/boom", func(c *fiber.Ctx) error { panic("explota") })

	resp, err := app.Test(httptest.NewRequest("GET", "/boom", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("panic debió dar 500, dio %d", resp.StatusCode)
	}
}

// TestMiddlewareRegistraExcepciones verifica la semántica de Excepciones:
// todo 5xx (respuesta directa, error devuelto o panic) genera UNA excepción en el
// span; un 4xx no; un 2xx tampoco. Se inspeccionan los spans reales con un
// SpanRecorder de OTel.
func TestMiddlewareRegistraExcepciones(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(prev)

	app := fiber.New()
	app.Use(Middleware())
	app.Get("/ok", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/err500", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "algo falló")
	})
	app.Get("/err400", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusBadRequest, "input inválido")
	})
	app.Get("/panic", func(c *fiber.Ctx) error { panic("explota") })

	for _, path := range []string{"/ok", "/err500", "/err400", "/panic"} {
		if _, err := app.Test(httptest.NewRequest("GET", path, nil)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}

	spans := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range sr.Ended() {
		spans[s.Name()] = s
	}
	excepciones := func(name string) int {
		s := spans[name]
		if s == nil {
			t.Fatalf("falta span %q", name)
		}
		n := 0
		for _, e := range s.Events() {
			if e.Name == "exception" {
				n++
			}
		}
		return n
	}

	if n := excepciones("GET /ok"); n != 0 {
		t.Errorf("GET /ok no debe generar excepción, generó %d", n)
	}
	if spans["GET /ok"].Status().Code == codes.Error {
		t.Errorf("GET /ok no debe tener status error")
	}
	if n := excepciones("GET /err500"); n != 1 {
		t.Errorf("GET /err500 debe generar 1 excepción, generó %d", n)
	}
	if spans["GET /err500"].Status().Code != codes.Error {
		t.Errorf("GET /err500 debe tener status error")
	}
	if n := excepciones("GET /err400"); n != 0 {
		t.Errorf("GET /err400 (4xx) no debe generar excepción, generó %d", n)
	}
	if n := excepciones("GET /panic"); n != 1 {
		t.Errorf("GET /panic debe generar exactamente 1 excepción (no duplicar), generó %d", n)
	}
	if spans["GET /panic"].Status().Code != codes.Error {
		t.Errorf("GET /panic debe tener status error")
	}
}
