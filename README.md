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
| `WithHashPepper` | `KEEPER_HASH_PEPPER` | vacío (PII → `***`; con pepper → hash `h1:…`) |
| `WithHashKeys` | — | `email,curp,rfc,vin,ssn` (solo identificadores; nunca secretos) |
| `WithContrato` | — | sin contrato: no se filtra nada (ver abajo) |
| `WithLogStdout` | `KEEPER_LOG_STDOUT` | apagado: los logs solo viajan a Keeper |
| `WithSinExportar` | — | apagado: exporta por OTLP |

Con `KEEPER_HASH_PEPPER` (el **mismo** en todos los servicios), los identificadores
sensibles se emiten como HMAC-SHA256 one-way (`h1:<hex>`) para correlacionar sin
exponer el dato. Los secretos (`password`/`token`/…) siguen censurándose con `***`.

## Contrato de telemetría (modo estricto)

La censura por nombre de campo no ve un nombre de persona guardado en un campo llamado
`persona`. El contrato cierra ese hueco: el servicio declara qué atributos puede emitir y
lo demás no sale del proceso.

```go
keeper.Start(ctx,
	keeper.WithContrato(keeper.Contrato{
		Modo: keeper.Descartar,
		Atributos: map[string]keeper.Clasificacion{
			"pld.desenlace":        keeper.Operativo,
			"pld.solicitante.curp": keeper.Identificador,
		},
	}),
)
```

- **Dónde se aplica:** en la cadena de logs (incluido `logger.With`), en el exportador de
  trazas (atributos del span y de sus eventos, también los que ponen las librerías de
  instrumentación) y en una vista sobre todas las métricas.
- **Qué siempre sale:** lo que emiten el SDK y `keeperfiber` (`request_id`, `http.*`,
  `url.path`, `client.*`, `exception.*`, `enduser.id`, `business.success`, …).
- **Clasificación:** `Operativo` sale tal cual. `Identificador` sale solo como hash `h1:…`
  con pepper, censurado sin pepper, y nunca en métricas.
- **Modos:** `Reportar` deja pasar lo no declarado y lo cuenta, para adoptar el contrato en
  un servicio que ya emite de todo. `Descartar` lo quita y lo cuenta.
- **Visibilidad:** `keeper.Violaciones()` devuelve cada clave fuera del contrato con su
  señal y su conteo; la métrica `keeper.contrato.violaciones` (atributos `keeper.contrato.senal` y
  `keeper.contrato.accion`, sin la clave) alimenta la alerta en Keeper; y el primer caso de cada clave se
  avisa en stderr.
- **Origen del contrato:** lo normal es generarlo desde un registro de convenciones
  semánticas en YAML (formato de OpenTelemetry Weaver), no escribirlo a mano.

**Prueba de contrato de un servicio:** arranca con `WithCaptura(c)` y el contrato, corre los
flujos con una persona sintética y falla si `keeper.Violaciones()` no está vacío o si
`c.Buscar(curp)` encuentra el dato en algún span, evento o log. La captura guarda lo que saldría
hacia Keeper, ya filtrado y censurado; implica `WithSinExportar`, así que las trazas se procesan
en síncrono y nada sale del proceso.

## Salida a consola

Sin `WithLogStdout` los logs solo viajan a Keeper y `docker logs` queda vacío. Con la opción
(o `KEEPER_LOG_STDOUT=true`), cada log se copia a stdout en JSON, ya filtrado y censurado,
con `trace_id` y `span_id` para ir de la consola a la traza.

## API

- `keeper.Start(ctx, opts...) (shutdown, error)` — inicializa trazas+métricas+logs (OTLP) y el logger.
- `keeper.Violaciones()` / `keeper.ReiniciarViolaciones()` — atributos fuera del contrato vistos y su conteo.
- `keeper.Logger() *slog.Logger` — logger estructurado; úsalo con el `ctx` del request para correlacionar.
- `keeper.LogError(ctx, msg, err, attrs...)` — loguea error con `exception.*` y lo registra en el span.
- `keeper.HashID(value)` — hash one-way manual (requiere pepper); normalmente lo hace el redact automático.

## Middleware Fiber (`keeperfiber`)

`keeperfiber.Middleware()` — **en producción**, validado en `kinetiq-api`. Por cada request:

- **`request_id`** (reusa el header entrante o genera un ULID) en contexto y respuesta;
- **span de servidor** propagando el trace context entrante (W3C `traceparent`);
- **log HTTP** con semántica OTel — 2xx/3xx a `debug` (la traza ya cubre el acceso), 4xx `warn`, 5xx `error`;
- **origen del cliente**: IP + navegador/SO/tipo de dispositivo (parseo del User-Agent con
  `mileusna/useragent`), inyectados en **todos** los logs del request
  (`client.address` · `client.browser` · `client.os` · `client.device.type` · `user_agent.original`);
- **excepciones**: `recover` de panics y `span.RecordError` en **toda respuesta 5xx** (usa el status
  efectivo aunque el handler devuelva el error) → pueblan la vista **Excepciones** de Keeper.

```go
app := fiber.New()
app.Use(keeperfiber.Middleware())
```

Si el servicio no registra identificadores en las rutas, `RutaParaTelemetria` decide qué ruta
ven `url.path`, `http.route`, el nombre del span y el log de cierre (por ejemplo, la plantilla
`/clients/:clientId` también cuando la petición se rechazó antes de resolver la ruta).
`RedactPathParams` saca el valor de parámetros sensibles del span **y** del log de cierre.

> Nota: no se emite `process.*` como resource (ruido para logs de negocio); sí `service.*` y `host.name`.

## Verificación

```bash
./init.sh   # build + vet + test
# smoke real contra una plataforma Keeper:
KEEPER_SMOKE_ENDPOINT=http://localhost:4318 go test -run TestSmoke ./...
```
