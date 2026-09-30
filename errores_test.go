package keeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// En Go los errores se loguean como `"error", err`. El SDK los lleva a la semántica de
// OpenTelemetry sin que el servicio reescriba cada llamada.
func TestErrores_ConvierteElAtributoConvencional(t *testing.T) {
	capt := nuevaCaptura()
	log := slog.New(erroresHandler{next: capt})
	causa := fmt.Errorf("consultar pendiente: %w", errors.New("conexión rechazada"))

	log.Error("no se pudo resolver", "pld.verdict_id", 7, "error", causa)

	attrs := capt.ultimo(t)
	if _, ok := attrs["error"]; ok {
		t.Fatal("la clave error no debe salir: se convierte en exception.*")
	}
	if attrs["exception.message"].String() != "consultar pendiente: conexión rechazada" {
		t.Errorf("exception.message = %q", attrs["exception.message"].String())
	}
	if attrs["exception.type"].String() != "*fmt.wrapError" {
		t.Errorf("exception.type = %q", attrs["exception.type"].String())
	}
	if attrs["pld.verdict_id"].Int64() != 7 {
		t.Error("los demás atributos deben conservarse")
	}
}

func TestErrores_MarcaElSpanSoloEnNivelError(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	log := slog.New(erroresHandler{next: nuevaCaptura()})

	ctx, advertido := tp.Tracer("p").Start(context.Background(), "advertencia")
	log.WarnContext(ctx, "reintento", "error", errors.New("tiempo agotado"))
	advertido.End()
	ctx, fallido := tp.Tracer("p").Start(context.Background(), "falla")
	log.ErrorContext(ctx, "no se pudo", "err", errors.New("tiempo agotado"))
	fallido.End()

	spans := sr.Ended()
	if spans[0].Status().Code == codes.Error || len(spans[0].Events()) != 0 {
		t.Error("una advertencia no marca el span como fallido")
	}
	if spans[1].Status().Code != codes.Error || len(spans[1].Events()) != 1 {
		t.Errorf("un log de error debe marcar el span y registrar la excepción: %+v", spans[1].Status())
	}
}

func TestErrores_TextoNilYVacio(t *testing.T) {
	capt := nuevaCaptura()
	log := slog.New(erroresHandler{next: capt})

	log.Info("aviso", "error", "tiempo de espera agotado")
	if capt.ultimo(t)["exception.message"].String() != "tiempo de espera agotado" {
		t.Error("un texto bajo la clave error va a exception.message")
	}
	var sinError error
	log.Info("aviso", "error", sinError, "otro", 1)
	attrs := capt.ultimo(t)
	if _, ok := attrs["exception.message"]; ok || len(attrs) != 1 {
		t.Errorf("un error nil no deja atributos: %v", attrs)
	}
	log.Info("aviso", "error", "")
	if len(capt.ultimo(t)) != 0 {
		t.Error("un texto vacío no deja atributos")
	}
}

func TestErrores_TambienConWith(t *testing.T) {
	capt := nuevaCaptura()
	slog.New(erroresHandler{next: capt}).With("error", errors.New("base caída")).Warn("degradado")
	if capt.ultimo(t)["exception.message"].String() != "base caída" {
		t.Fatalf("logger.With debe convertir el error: %v", capt.ultimo(t))
	}
}

func TestErrores_SinErrorNoTocaElRegistro(t *testing.T) {
	capt := nuevaCaptura()
	slog.New(erroresHandler{next: capt}).Info("hola", "a", 1)
	if attrs := capt.ultimo(t); len(attrs) != 1 || attrs["a"].Int64() != 1 {
		t.Fatalf("un log sin error pasa intacto: %v", attrs)
	}
}
