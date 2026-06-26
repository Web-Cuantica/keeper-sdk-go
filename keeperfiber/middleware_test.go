package keeperfiber

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
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
