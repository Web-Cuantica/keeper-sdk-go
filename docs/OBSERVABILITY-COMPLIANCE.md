# Backlog de cumplimiento de observabilidad — keeper-sdk-go

> **Origen:** auditoría del SDK contra el estándar `OBSERVABILITY-ENGINEERING.md`
> (skill `observability-advisor`, Modo 2). Este documento es el **backlog ejecutable**;
> las reglas citadas (`§X`, `MUST`/`SHOULD`) refieren a las secciones de ese estándar.
> Fecha de auditoría: 2026-07-20. Última ejecución: 2026-07-20.

## Alcance

`keeper-sdk-go` es una **librería de instrumentación** (§12.2 "librería interna reutilizable").
Por tanto solo le aplican las reglas de: emisión del evento (§3), trazas (§4), semconv (§5),
seguridad del evento (§3.4), muestreo (§7) y anti-lock-in (§13.2). **No** le aplican SLOs/alertas
(§6), pipeline/collector (§8) ni proceso de PR/post-deploy (§12.1).

## Estado de ejecución

Validado con `go test -cover ./...`:

| Paquete | Cobertura |
|---------|-----------|
| `keeper` | **65.4%** (`Start()` requiere OTLP vivo → smoke) |
| `keeperfiber` | **83.5%** |

- [x] **GO-0** — `OBSERVABILITY-ENGINEERING.md` + `.cursor/rules/…` + `AGENTS.md`/`CLAUDE.md`.
- [x] **GO-1** — evento ancho: `ContextWithEvent` / `Annotate` / `EventAttrs`; middleware vuelca al span + log.
- [x] **GO-2** — `WithSampleRatio` + `OTEL_TRACES_SAMPLER(_ARG)`; `sample_rate` en span/log.
- [x] **GO-3** — redacción por subcadena + recursiva en grupos slog; `RedactAttrs`.
- [x] **GO-4** — `build_id` / `commit_hash` en el resource.
- [x] **GO-5** — `AnnotateUser` / `AnnotateTenant` (`enduser.id` / `tenant.id`).
- [x] **GO-6** — `AnnotateOutcome` → `business.success` + `error.kind` / `error.message`.
- [x] **GO-7** — `InjectOutbound(ctx, headers)` propaga W3C `traceparent` + `X-Request-ID`.
- [x] **GO-8** — `Middleware(MiddlewareConfig{IgnorePaths})`; default `/health|/healthz|/live|/ready|/ping|/metrics`.
- [x] **GO-9** — hash one-way HMAC-SHA256 (`WithHashPepper` / `KEEPER_HASH_PEPPER`); claves
  `email/curp/rfc/vin/ssn` → `h1:<hex>`; secretos siguen en `***`.
- [ ] **GO-10** — helpers GenAI semconv (`gen_ai.*`) si hay uso real de LLMs.

## Pendiente (roadmap)

### GO-10 · Observabilidad de IA — §10 (`SHOULD`)
- [ ] Helpers GenAI semconv + costo a query-time. (Depende del uso real.)

> Registrar en un ADR (§17) decisiones relevantes (evento canónico, muestreo, contrato de redacción).
