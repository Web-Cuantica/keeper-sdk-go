# keeper-sdk-go

SDK de observabilidad de **Keeper** para **Go**: trazas, métricas y **logs "bien armados"**
sobre OpenTelemetry, exportados por OTLP a la plataforma Keeper (SigNoz).

Implementa el **Keeper Logging Standard** (`keeper/docs/adr/0014-...`): mensaje legible +
atributos planos, semántica OTel, correlación por traza y **redacción de PII/secrets** —
la misma disciplina de `@dy/logging`, en idioma Go.

## Instalación

```bash
go get github.com/Web-Cuantica/keeper-sdk-go
```

## Uso

```go
package main

import (
	"context"
	"log/slog"

	keeper "github.com/Web-Cuantica/keeper-sdk-go"
)

func main() {
	ctx := context.Background()
	shutdown, err := keeper.Start(ctx,
		keeper.WithService("kinetiq-api"),
		keeper.WithEnv("production"),
		keeper.WithVersion("1.4.2"),
		keeper.WithEndpoint("http://keeper-host:4318"),
	)
	if err != nil {
		panic(err)
	}
	defer shutdown(ctx)

	// Mensaje para humanos + atributos planos (filtrables en Keeper).
	keeper.Logger().InfoContext(ctx, "Recibo aprobado",
		slog.Int("rcpt_id", 87772),
		slog.String("sales_order", "1345678"),
	)

	// Errores: registra exception.* y marca el span activo.
	// keeper.LogError(ctx, "Fallo al aprobar", err, slog.Int("rcpt_id", 87772))
}
```

Produce un log correlacionado con la traza activa, con `service.name`/`deployment.environment`/
`service.version` como resource, severidad OTel y el `password`/`token`/etc. **censurados**.

## Configuración (opción o variable de entorno)

| Opción | Env | Default |
|---|---|---|
| `WithService` | `KEEPER_SERVICE_NAME` / `SERVICE_NAME` | `unknown-service` |
| `WithEnv` | `KEEPER_ENV` / `NODE_ENV` | `development` |
| `WithVersion` | `KEEPER_SERVICE_VERSION` / `APP_VERSION` | `0.0.0` |
| `WithEndpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` |
| `WithLevel` | `KEEPER_LOG_LEVEL` | dev→debug, prod→info |
| `WithRedactKeys` | — | `authorization,password,token,secret,vin,email,...` |

## API

- `keeper.Start(ctx, opts...) (shutdown, error)` — inicializa trazas+métricas+logs (OTLP) y el logger.
- `keeper.Logger() *slog.Logger` — logger estructurado; úsalo con el `ctx` del request para correlacionar.
- `keeper.LogError(ctx, msg, err, attrs...)` — loguea error con `exception.*` y lo registra en el span.

> Middleware para **Fiber** (`request_id`, propagación de trace, log HTTP estándar): subpaquete
> `keeperfiber` (en construcción; objetivo de validación: `kinetiq-api`).

## Verificación

```bash
./init.sh   # build + vet + test
# smoke real contra una plataforma Keeper:
KEEPER_SMOKE_ENDPOINT=http://localhost:4318 go test -run TestSmoke ./...
```
