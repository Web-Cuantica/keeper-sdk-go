// Ejemplo: emite un log por cada nivel para ver severidad en la plataforma.
// Uso: KEEPER_SERVICE_NAME=keeper-niveles OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./examples/levels
package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	keeper "github.com/Web-Cuantica/keeper-sdk-go"
)

func main() {
	ctx := context.Background()
	shutdown, err := keeper.Start(ctx,
		keeper.WithService("keeper-sdk-go-niveles"),
		keeper.WithEnv("dev"),
		keeper.WithLevel("debug"),
	)
	if err != nil {
		panic(err)
	}
	log := keeper.Logger()

	log.DebugContext(ctx, "prueba DEBUG", slog.String("nivel", "debug"))
	log.InfoContext(ctx, "prueba INFO", slog.String("nivel", "info"))
	log.WarnContext(ctx, "prueba WARN", slog.String("nivel", "warn"))
	log.ErrorContext(ctx, "prueba ERROR", slog.String("nivel", "error"))
	keeper.LogError(ctx, "prueba ERROR vía LogError", errors.New("boom"),
		slog.String("nivel", "error"))
	log.LogAttrs(ctx, slog.Level(12), "prueba FATAL", slog.String("nivel", "fatal"))

	time.Sleep(500 * time.Millisecond)
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = shutdown(sctx)
}
