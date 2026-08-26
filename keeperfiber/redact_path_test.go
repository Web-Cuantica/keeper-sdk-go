package keeperfiber

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
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
