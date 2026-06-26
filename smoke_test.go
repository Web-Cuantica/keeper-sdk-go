package keeper

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
)

// TestSmoke valida la cadena real contra una plataforma Keeper. Hace skip si no hay
// KEEPER_SMOKE_ENDPOINT; cuando está, emite una traza, un log de negocio (con un
// secret para verificar la redacción) y un error, y los exporta por OTLP.
func TestSmoke(t *testing.T) {
	ep := os.Getenv("KEEPER_SMOKE_ENDPOINT")
	if ep == "" {
		t.Skip("KEEPER_SMOKE_ENDPOINT no definido; omito smoke de integración")
	}
	ctx := context.Background()
	shutdown, err := Start(ctx,
		WithService("keeper-sdk-go-smoke"),
		WithEndpoint(ep),
		WithEnv("dev"),
		WithVersion("0.0.1"),
	)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, span := otel.Tracer("smoke").Start(ctx, "smoke-op")
	Logger().InfoContext(ctx, "Hola Keeper desde Go",
		slog.Int("inspection_id", 97125),
		slog.String("zone_type", "LAT-IZQ"),
		slog.String("password", "supersecreto"), // debe salir como ***
	)
	LogError(ctx, "fallo simulado al guardar daño", errors.New("lock timeout"),
		slog.Int("inspection_id", 97125))
	span.End()

	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
