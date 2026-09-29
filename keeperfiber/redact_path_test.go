package keeperfiber

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Un token que viaja por la URL es una credencial: si queda en la telemetría,
// quien pueda leer las trazas puede usarla.
func TestRedactPathParams_ElTokenNoSaleEnElPath(t *testing.T) {
	app := fiber.New()
	var visto string

	app.Get("/api/v1/public/quotes/:token", func(c *fiber.Ctx) error {
		visto = redactPathParams(c, c.Path(), []string{"token"})
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/public/quotes/abc123SECRETO", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("la petición falló: %v", err)
	}

	if visto != "/api/v1/public/quotes/:token" {
		t.Errorf("se esperaba el path con el parámetro redactado, se obtuvo %q", visto)
	}
}

// El resto de la URL se conserva: redactar no puede volver ilegible una ruta.
func TestRedactPathParams_ConservaLosDemasSegmentos(t *testing.T) {
	app := fiber.New()
	var visto string

	app.Get("/api/v1/public/quotes/:token/pdf", func(c *fiber.Ctx) error {
		visto = redactPathParams(c, c.Path(), []string{"token"})
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/public/quotes/tok-999/pdf", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("la petición falló: %v", err)
	}

	if visto != "/api/v1/public/quotes/:token/pdf" {
		t.Errorf("se esperaba /pdf al final, se obtuvo %q", visto)
	}
}

// Sin parámetros configurados el path sale intacto: la redacción es opt-in.
func TestRedactPathParams_SinConfiguracionNoTocaNada(t *testing.T) {
	app := fiber.New()
	var visto string

	app.Get("/api/v1/quotation/:id", func(c *fiber.Ctx) error {
		visto = redactPathParams(c, c.Path(), nil)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/quotation/42", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("la petición falló: %v", err)
	}

	if visto != "/api/v1/quotation/42" {
		t.Errorf("sin configuración el path no se toca, se obtuvo %q", visto)
	}
}

// Se sustituye por segmento completo: un valor corto que aparezca por casualidad
// dentro de otro segmento no debe romper una ruta legítima de leer.
func TestRedactPathParams_SustituyePorSegmentoCompleto(t *testing.T) {
	app := fiber.New()
	var visto string

	app.Get("/api/v1/pdf/:token/pdf", func(c *fiber.Ctx) error {
		visto = redactPathParams(c, c.Path(), []string{"token"})
		return c.SendStatus(fiber.StatusOK)
	})

	// El valor del token es "pdf": coincide con otro segmento de la ruta.
	req := httptest.NewRequest("GET", "/api/v1/pdf/pdf/pdf", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("la petición falló: %v", err)
	}

	// Los tres segmentos "pdf" son iguales, así que los tres se redactan: es el
	// comportamiento seguro (de más, nunca de menos) ante un valor ambiguo.
	if visto != "/api/v1/:token/:token/:token" {
		t.Errorf("ante un valor ambiguo se redacta de más, se obtuvo %q", visto)
	}
}

// Un parámetro que la ruta no tiene se ignora sin romper nada.
func TestRedactPathParams_ParametroInexistente(t *testing.T) {
	app := fiber.New()
	var visto string

	app.Get("/api/v1/quotation/:id", func(c *fiber.Ctx) error {
		visto = redactPathParams(c, c.Path(), []string{"token"})
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/v1/quotation/42", nil)
	if _, err := app.Test(req); err != nil {
		t.Fatalf("la petición falló: %v", err)
	}

	if visto != "/api/v1/quotation/42" {
		t.Errorf("se obtuvo %q", visto)
	}
}

func conLogsEn(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// Regresión: RedactPathParams prometía sacar el valor del log de cierre y ese log usaba la ruta
// cruda. El secreto seguía llegando a Keeper por la vista de Logs.
func TestRedactPathParams_TambienEnElLogDeCierre(t *testing.T) {
	logs := conLogsEn(t)
	app := fiber.New()
	app.Use(Middleware(MiddlewareConfig{RedactPathParams: []string{"token"}, LogSuccess: true}))
	app.Get("/api/v1/public/quotes/:token", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	if _, err := app.Test(httptest.NewRequest("GET", "/api/v1/public/quotes/abc123SECRETO", nil)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "abc123SECRETO") {
		t.Fatalf("el log de cierre filtró el token:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "/api/v1/public/quotes/:token") {
		t.Fatalf("el log de cierre debe llevar la ruta redactada:\n%s", logs.String())
	}
}

// Un servicio que no registra identificadores decide qué ruta ve la telemetría, también cuando
// la petición se rechaza antes de que el router la resuelva.
func TestRutaParaTelemetria_MandaSobreSpanYLog(t *testing.T) {
	logs := conLogsEn(t)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	app := fiber.New()
	app.Use(Middleware(MiddlewareConfig{
		LogSuccess:         true,
		RutaParaTelemetria: func(*fiber.Ctx) string { return "/api/v1/clients/:clientId" },
	}))
	api := app.Group("/api/v1", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusUnauthorized) })
	api.Get("/clients/:clientId", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	if _, err := app.Test(httptest.NewRequest("GET", "/api/v1/clients/zq7731x", nil)); err != nil {
		t.Fatal(err)
	}
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("esperaba un span, hubo %d", len(spans))
	}
	s := spans[0]
	if s.Name() != "GET /api/v1/clients/:clientId" {
		t.Errorf("nombre del span: %q", s.Name())
	}
	for _, kv := range s.Attributes() {
		if strings.Contains(kv.Value.Emit(), "zq7731x") {
			t.Errorf("el id salió en el atributo %s del span", kv.Key)
		}
	}
	if strings.Contains(logs.String(), "zq7731x") {
		t.Fatalf("el id salió en el log de cierre:\n%s", logs.String())
	}
}
