package keeperfiber

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddlewareGeneraRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(Middleware())
	app.Get("/ping", func(c *fiber.Ctx) error { return c.SendString("pong") })

	resp, err := app.Test(httptest.NewRequest("GET", "/ping", nil))
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
	app.Get("/ping", func(c *fiber.Ctx) error { return c.SendString("pong") })

	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set(RequestIDHeader, "rid-fijo-123")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := resp.Header.Get(RequestIDHeader); got != "rid-fijo-123" {
		t.Errorf("request_id no reutilizado: %q", got)
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
