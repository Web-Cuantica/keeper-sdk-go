# AGENTS.md

Este repositorio (`keeper-sdk-go`) es una librería de instrumentación de observabilidad
(logs estructurados, trazas y métricas sobre OpenTelemetry, export OTLP).

## Observabilidad e instrumentación

**Antes de instrumentar código, tocar el logging o diseñar SLOs/alertas, lee y aplica los estándares de:**

➡️ [`OBSERVABILITY-ENGINEERING.md`](./OBSERVABILITY-ENGINEERING.md)

Ese documento es la **fuente de verdad** sobre el evento ancho estructurado y sus campos base, trazas y propagación de contexto, convenciones semánticas, SLOs y burn alerts, muestreo, pipelines de telemetría, el ciclo de análisis central, observabilidad de IA (LLMs/agentes), feedback loops ("test in prod"), prácticas de equipo (ODD), gobernanza (costo vs inversión, build vs buy) y la plantilla de decisiones (ADR). Es agnóstico de herramienta (aplica a Codex, Cursor, Claude Code, etc.).

El backlog de cumplimiento vive en [`docs/OBSERVABILITY-COMPLIANCE.md`](./docs/OBSERVABILITY-COMPLIANCE.md).
