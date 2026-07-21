# OBSERVABILITY-ENGINEERING.md — Estándar de observabilidad e instrumentación

> **Fuente de verdad, agnóstica de herramienta** (Claude Code, Cursor, Codex, Copilot) y de backend
> (Honeycomb, DataDog, Grafana, SigNoz, ClickHouse, Elastic...). Destilado de *Observability
> Engineering, 2ª edición* (Charity Majors, Liz Fong-Jones, George Miranda, con Austin Parker —
> O'Reilly, 2026). Cubre cómo emitir telemetría útil (eventos anchos estructurados, trazas), gobernar
> el lenguaje de la telemetría (convenciones semánticas y esquemas), diseñar SLOs y alertas,
> muestrear, operar pipelines, depurar con datos, observar IA (LLMs/agentes) y sostener la práctica
> como inversión organizacional. Cualquier IA que trabaje en este repo debe leer y respetar este
> documento antes de instrumentar código, tocar el logging, diseñar alertas o generar código que irá
> a producción.
>
> Convención de reglas: **`MUST`** = obligatorio (no violar sin ADR). **`SHOULD`** = recomendado
> fuerte. **`AVOID`** = anti-patrón conocido.
>
> **Tesis de la 2ª edición (era de la IA):** cuando los agentes generan código más rápido de lo que
> los humanos pueden revisarlo, se colapsan las compuertas clásicas (design review, code review, QA,
> release sign-off). **La observabilidad es la única que sobrevive y se vuelve el mecanismo de
> seguridad primario del ciclo de vida.** Es una **propiedad de la confiabilidad** y el piso de la
> velocidad de aprendizaje de la organización.
>
> **Documentos hermanos (no se duplican, se referencian):**
> - `AI-ENGINEERING.md` — metodología de la *feature* de IA: evaluación, prompts, RAG, guardrails,
>   inferencia.
> - `MCP-ENGINEERING.md` — el *protocolo* de tools (MCP): servers/clients, seguridad, testing, deploy.
> - `AGENT-ENGINEERING.md` — el *sistema agéntico*: ciclo de vida completo
>   (decidir→diseñar→validar→operar→proteger→gobernar).

---

## 1. Decisión previa: ¿monitoreo, observabilidad o ambos?

No compiten: responden preguntas distintas. El monitoreo cubre los **known-unknowns** (fallos
conocidos, umbrales); la observabilidad cubre los **unknown-unknowns** (estados novedosos que nadie
previó). La observabilidad moderna es una **propiedad de la confiabilidad (*dependability*)** — al
lado de disponibilidad, fiabilidad, mantenibilidad, seguridad, confidencialidad e integridad — no un
producto que se compra.

| Situación | Usa | Por qué |
|-----------|-----|---------|
| Infraestructura que TÚ gestionas (VMs, k8s, BD self-hosted) | Monitoreo (métricas agregadas + alertas) | Cambia lento y predecible; sus modos de fallo son conocidos |
| **Tu código** en producción | Observabilidad (eventos anchos + trazas) | Cambia a diario; falla de formas novedosas |
| **Código generado por IA / workflows agénticos** | Observabilidad, sin excepción | El no-determinismo es una *feature*; la única forma de saber qué hizo un agente es instrumentar y observar producción |
| PaaS/serverless (casi sin infra propia) | Casi solo observabilidad | El proveedor ya monitorea su infraestructura |
| Tendencias históricas agregadas (¿subió el p95 en 18 meses?) | Monitoreo/métricas | Ni BI ni observabilidad las resuelven bien |
| Exactitud total y retención eterna (facturación, auditoría) | BI / almacén analítico | La observabilidad es rápida y aproximada, no exacta ni eterna |

- `MUST` Dimensiona cuánto monitoreo necesitas según cuánta infraestructura gestionas tú mismo
  (bare-metal → mucho; IaaS → medio; PaaS/serverless → poco o nada). La observabilidad de tu
  software se necesita en **todos** los casos.
- `SHOULD` Captura CPU/memoria/disco del host **dentro de tus eventos** (métricas de orden
  superior): son alerta temprana de que el código golpea límites físicos (ej. un deploy que triplica
  la memoria residente). Ignora las miles de variables de bajo nivel de `/proc`.
- `AVOID` Tirar el monitoreo existente que funciona al adoptar observabilidad. Coexisten.
- `AVOID` "Modernizar" un sistema de monitoreo tradicional añadiéndole funciones de observabilidad
  encima: su modelo de datos preagregado no soporta cardinalidad ni exploración ad hoc.
- `AVOID` Equiparar observabilidad con "tres pilares" (métricas + logs + trazas): es eslogan de
  vendors que ignora el análisis y **destruye las relaciones entre datos en tiempo de escritura**. Si
  quieres tres pilares reales: **alta cardinalidad + alta dimensionalidad + explorabilidad**.

### 1.1 Modelo unificado vs. tres pilares (elige por feedback loop)

| | **Tres pilares** | **Almacenamiento unificado** |
|---|---|---|
| Origen | monitoreo/logging | BI + APM |
| Sirve al | loop **operacional** (infra, salud del sistema) | loop de **aprendizaje del developer** (calidad, UX, cada cambio) |
| Datos | métricas/logs/trazas en silos; correlación manual; relaciones destruidas al escribir | eventos estructurados en columnar; relaciones preservadas; alta cardinalidad/dimensionalidad |
| Brilla en | monitoreo de infraestructura | preguntas exploratorias ("todos los requests de móviles en la UE con flag X y error en checkout") |

- `MUST` Antes de elegir tooling, alinea en **una** pregunta: *¿tu cuello de botella es dev u ops?*
  Dolor operacional → herramientas de outcomes operacionales; dolor de velocidad / avalancha de
  código IA → modelo unificado de eventos estructurados.
- `SHOULD` Recuerda que **precisión = riqueza de contexto × cardinalidad**: con 29 campos en un
  evento, el campo 30 vale más que los 29 juntos (combinaciones exponenciales).

---

## 2. Principios no negociables

1. **El evento ancho estructurado es la unidad de telemetría** — un evento por unidad de trabajo con
   todo su contexto, no N logs sueltos ni métricas preagregadas.
2. **La alta cardinalidad es la materia prima** — los campos más útiles para depurar son los de más
   valores únicos (`user_id`, `request_id`, ids de negocio). Captúralos siempre.
3. **Agrega en consulta, nunca en escritura** — puedes agrupar después lo crudo; jamás recuperar lo
   que preagregaste.
4. **Alerta por síntomas del usuario, no por causas** — el "qué" dispara la alerta; el "por qué" se
   investiga con observabilidad.
5. **La instrumentación acompaña al código** — mismo PR, misma revisión. La pregunta obligada:
   *"¿cómo sabré si este cambio funciona como espero?"*.
6. **Instrumenta con estándares abiertos (OpenTelemetry)** — instrumentar es la parte cara; que sea
   reutilizable entre backends. Nada de agentes propietarios como base.
7. **Percentiles, no promedios** — p50/p95/p99 por evento; y aun así, los percentiles agregados no
   sustituyen el acceso al evento individual.
8. **Muestrea, no preagregues** — pasada cierta escala, muestreo con `sample_rate` dentro del
   evento; la preagregación destruye el desglose.
9. **Producción es la fuente de verdad** — staging jamás la replica. *Test in prod, o vive una
   mentira.* Invierte en entender producción, no solo en endurecer pre-producción.
10. **Es una práctica y una inversión, no una compra** — como la testabilidad: se cultiva con
    inversión continua, hábitos y cultura. Hereda las propiedades del código que observa (si el
    código genera ingresos, observarlo es inversión, no centro de costo).
11. **La observabilidad es un flujo de información que cierra feedback loops** — es el mecanismo de
    *sensemaking* que conecta causa y efecto; sin ella no hay loop, solo caos.

---

## 3. El evento ancho estructurado

### 3.1 Un evento por unidad de trabajo

El modelo canónico es el **"blob vacío"**:

1. Al entrar la solicitud (o iniciar el job/mensaje), inicializa un mapa vacío.
2. Durante toda su vida, **acumula** en él todo lo relevante: ids, parámetros, resultados
   intermedios, contadores, tiempos de llamadas remotas, flags.
3. Al terminar (éxito **o fallo**), emite el mapa completo como **UN** evento estructurado.

- `MUST` Emite un evento ancho por unidad de trabajo (request HTTP, mensaje de cola, archivo
  procesado, objeto de un batch), con el contexto completo en pares clave-valor.
- `MUST` Captura ambos niveles: datos del entorno (runtime, contenedor, versión) **y** datos por
  solicitud (ids de negocio, usuario, parámetros).
- `MUST` No pongas límite práctico al número de campos: con instrumentación madura son comunes
  **300-400 dimensiones por evento**. Seis campos es monitoreo, no observabilidad.
- `SHOULD` Prefiere **desgloses de tiempo dentro de un evento ancho** antes que multiplicar spans
  hijos: añade `db.duration_ms`, `render.duration_ms`, etc. al evento en vez de crear un span por
  cada micro-operación (menos overhead, mismo poder analítico).
- `SHOULD` Si hoy emites N logs sueltos por request, redirígelos: **anota el evento canónico** y
  deja el log suelto solo para debug local.
- `AVOID` Partir la unidad de trabajo en muchas líneas narrativas sin estructura: a escala son
  ruido, exigen parsers frágiles y solo sirven cuando ya sospechas la causa.
- `AVOID` La "carrera armamentística de métricas": añadir métricas custom para reconstruir
  comportamiento por solicitud. No escala y desconecta el contexto.

### 3.2 Campos base del evento

El esquema **base** es obligatorio; las dimensiones adicionales son libres (añádelas sin ceremonia
ni migración).

| Grupo | Campos | Notas |
|-------|--------|-------|
| Correlación | `timestamp`, `trace_id`, `span_id`, `parent_id`, `request_id` | `timestamp` ISO-8601 UTC del momento del evento — va en **todo** evento; `parent_id` nulo identifica la raíz |
| Servicio y deploy | `service`, `env`, `version`, `build_id`/`commit_hash`, `hostname`/`pod`, `instance_type`, `availability_zone` | Permiten responder "¿es de este deploy / este nodo / esta zona?" |
| Feature flags | `feature_flags` activos, grupo de canary | Los flags crean combinaciones de estado imposibles de probar antes |
| Request | `http.method`, `http.route` (normalizada, `/receipt/:id`), `http.url` (cruda), `http.status_code`, `duration` (unidad explícita), `client_ip`, `user_agent` | `http.route` agrupa SLIs por endpoint; `http.url` conserva la cardinalidad |
| Usuario y negocio | `user_id`, `tenant_id`, ids de negocio (`order_id`, `cart_id`...), backend/dependencia destino, `shard`, query normalizada, contadores y resultados intermedios | Lo más valioso: solo tu código lo conoce |
| Resultado | éxito/fallo **de negocio** (no solo HTTP), `error.kind`, `error.message`, `error.stack` | Un HTTP 200 lento o mal procesado es un evento malo |
| Muestreo | `sample_rate` | Cuántos eventos representa este (ver §7) |
| Tráfico de prueba | marca `synthetic`/`test` | Distingue E2E y sondas sintéticas del tráfico real |

- `MUST` Registra la versión/build del servicio en cada evento: con varias versiones simultáneas en
  producción es lo único que atribuye una regresión a su deploy.
- `SHOULD` Registra medidas de negocio (ej. valor del carrito) **como atributos del evento/span** al
  que pertenecen, no en un sistema de métricas aparte.
- `SHOULD` Etiqueta el tráfico generado por pruebas E2E/sintéticas para monitorizarlo con la misma
  telemetría sin contaminar los datos reales.

### 3.3 Nombres y esquema

- `MUST` Convención de nombres **sí**, límite de campos **no**: nombres estables y compartidos entre
  servicios (mismo concepto = mismo nombre), campos ilimitados. (Gobierna el vocabulario con §5.)
- `MUST` Nunca generes nombres de campo dinámicos o de un solo uso (`timestamp_20260619: true`): el
  dato variable va como **valor**, el nombre genérico como **clave**. Las columnas de un solo uso
  destruyen la economía del almacén columnar.
- `SHOULD` Gestiona el esquema con constantes en el código a pequeña escala; con esquemas de
  telemetría versionados y pipelines de normalización a gran escala (§5.2).
- `AVOID` Esquemas rígidos predefinidos que exijan migración para añadir una dimensión: predecir qué
  necesitarás es lo contrario de observabilidad.

### 3.4 Seguridad del evento

- `MUST` Nunca emitas secrets, tokens ni PII sin enmascarar; redacta **antes** de que el dato salga
  del proceso, y otra vez en el pipeline (§8) como red de seguridad.
- `MUST` Conoce el alcance real de tu redacción (¿cubre claves anidadas a N niveles?) y documenta
  sus límites.
- `SHOULD` En entornos regulados, usa **hashes one-way consistentes** en toda la organización para
  identificadores sensibles: ganas correlación sin exponer el dato (seguridad prospera con
  aislamiento; compliance con evidencia; observabilidad con correlación — reconcílialos así).
- `AVOID` Loguear payloads o respuestas completas: extrae las dimensiones útiles.

---

## 4. Trazas: eventos anchos conectados

### 4.1 Campos y propagación

- `MUST` Cada span lleva los **5 campos obligatorios**: `trace_id`, `span_id`, `parent_id`,
  `timestamp`, `duration` (+ recomendados: `service_name` y nombre del span).
- `MUST` Propaga el contexto en toda llamada saliente con un estándar: **W3C Trace Context**
  (`traceparent`) o B3. Sin propagación, la traza se corta en cada frontera.
- `MUST` Enriquece cada span como si fuera un wide event (§3.2): la cascada sola dice *dónde*; el
  contexto dice *por qué*.

### 4.2 Spans propios (instrumentación personalizada)

- `MUST` No dependas solo de la auto-instrumentación: cubre los bordes (HTTP, BD, colas), no tu
  lógica de negocio. Arranca con ella (valor en horas), y añade atributos y spans propios desde el
  primer día.
- `SHOULD` Crea un span solo cuando sea **interesante y agregable** (una operación que quieras
  medir/agrupar); para el resto, añade atributos/desgloses de tiempo al evento padre (§3.1).
- `SHOULD` Traza unidades no-RPC: un span por objeto de un batch, por fase de un pipeline, y patrones
  especiales (streaming, async, serverless, eBPF) con telemetría por capas.
- `SHOULD` Ante un span misterioso lento, añade sub-spans o atributos que lo expliquen, en vez de
  especular. La instrumentación es la mejor documentación del sistema.
- `SHOULD` Modela procesos largos (imports, colas de días) por **sondeo con eventos periódicos**, no
  con una traza única: la ventana sana de una traza es de segundos a minutos.

### 4.3 Métricas de aplicación: solo 3 casos

- `AVOID` Usar un meter/métrica de aplicación salvo que necesites exactamente: (1) conteo exacto
  **sin muestreo**, (2) valor preagregado por dimensiones fijas, o (3) valor de proceso no ligado a
  una solicitud, leído periódicamente (ej. goroutines/heap). Todo lo demás va como atributo del
  evento.
- `AVOID` Dimensiones de alta cardinalidad (`user_id`, IP, hostname a escala) como etiquetas de
  métricas: cada combinación crea una serie temporal nueva → explosión de cardinalidad y de factura.

---

## 5. Gobernar el lenguaje: convenciones semánticas y esquemas

La *estrella polar* de un equipo de observabilidad es **definir y gestionar el lenguaje construido de
la organización** — un puente entre la jerga interna y la *lingua franca* del mundo. Esto importa más
que nunca porque **la IA hace pattern-matching agresivo** sobre los nombres: un vocabulario ambiguo
produce dashboards en conflicto y agentes que incluyen/excluyen datos incorrectamente.

### 5.1 Convenciones semánticas (OTel semconv)

- `MUST` Usa las **OpenTelemetry Semantic Conventions** como base del vocabulario: `http.request.method`,
  `db.system`, `exception.type`, etc. Hacen la observabilidad una habilidad **entrenable y portable**.
- `SHOULD` Sigue la gramática: namespaces **de específico a general, de izquierda a derecha**; actores
  por **client/server o producer/consumer** (no acoples al detalle de transporte, que cambia).
- `SHOULD` Coloca los atributos de negocio compartidos al nivel correcto del namespace: un
  `transaction.id` compartido en toda la organización va más alto que uno específico de servicio
  (`acme.transaction.id` vs. `acme.<servicio>.transaction.id`).

### 5.2 Esquemas de telemetría (opcional a pequeña escala, valioso a gran escala)

- `SHOULD` Trata la telemetría como una **API con contrato y versionado**: un esquema textual de lo
  que un servicio emite, cerca del código, con tooling (ej. **Weaver**) para descubrir, lintar,
  generar y verificar en compile-time/runtime.
- `SHOULD` Comparte atributos entre señales (**DRY**): un atributo de alta cardinalidad
  (`transaction.id`) no va en métricas de time series; estratifica con uno de baja cardinalidad
  (`customer.region`/tier) para navegar entre señales.
- `SHOULD` En entornos regulados, corre la validación de esquema en **CI** para generar evidencia de
  compliance y evitar fugas de datos sensibles.

### 5.3 Ontologías (semántica compartida humanos + IA)

- `SHOULD` Cuando distintos equipos interpretan los mismos términos de forma distinta (dashboards en
  conflicto), formaliza una **ontología**: un mapa de entidades, relaciones y reglas del dominio,
  operacionalizado vía convenciones semánticas. Da *guardrails* a los sistemas de IA no deterministas
  y cierra el loop entre la telemetría de producción y la lógica de validación.

---

## 6. SLOs, error budgets y alertas

### 6.1 Los dos criterios de toda alerta

- `MUST` Toda alerta paginable cumple ambos: (1) es indicador fiable de **experiencia de usuario
  degradada**, y (2) es **accionable** con un camino sistemático de depuración. Lo que no cumpla
  ambos, se borra.
- `MUST` No despiertes a nadie por fallos que se auto-reparan (reintentos, failover, autoescalado, 1
  nodo caído): se revisan en horario laboral.
- `AVOID` Alertas por causas fáciles de medir (CPU > 80 %, memoria < 10 %, disco): baratas de
  recolectar, llenas de falsos positivos → fatiga de alertas.
- `AVOID` Añadir una alerta "importante" tras cada post-mortem, y tolerar alertas ruidosas ("esa
  siempre suena, ignórala"): es la **normalización de la desviación**. Bórralas.
- `AVOID` Umbrales estáticos (p95 > N ms, "10 usuarios lentos") en sistemas cuyo tráfico varía por
  hora y zona horaria.
- `SHOULD` Embebe el triage en la alerta: consulta parametrizada por la dimensión afectada + enlaces
  al runner/trazas. Una alerta que solo avisa no es accionable.

### 6.2 SLIs por evento

- `MUST` Define cada SLI clasificando **cada evento individual** como bueno o malo con criterios
  explícitos: ruta elegible + umbral de duración + éxito de procesamiento. Un 200 lento cuenta como
  malo.
- `SHOULD` Prefiere SLIs por evento sobre SLIs por buckets de tiempo: con el 94 % de solicitudes
  sanas, el bucket de 1 min se marca 100 % malo; por evento solo se descuenta el 6 %. Con SLOs
  estrictos (≥ 99.99 %) los eventos son obligatorios.
- `SHOULD` Define el SLO interno más estricto que el SLA externo, y con parámetros que capturen
  fallas parciales por segmento (plataforma, región, flag): las interrupciones rara vez son
  binarias.

### 6.3 Error budget y burn alerts

- `MUST` Calcula el presupuesto sobre **ventana deslizante de 30 días** (ni calendario que se
  resetea el día 1, ni 7-14 días, ni 90).
- `MUST` Alerta por **proyección de agotamiento** (burn alerts), no por presupuesto ya agotado ni
  por "queda < X %".
- `MUST` Extrapola **proporcionalmente** (tasa de fallo observada × tráfico esperado), no
  linealmente: 25 fallas de madrugada al 50 % proyectan 105 lineales ("todo bien") pero 720
  proporcionales (presupuesto agotado en medio día).
- `MUST` Respeta el **factor de 4**: una ventana de referencia solo soporta proyecciones hasta 4× su
  duración (alarma de 24 h ← datos de 6 h; alarma de 4 h ← datos de 1 h). Desproporciones causan
  flapping o detección tardía.
- `SHOULD` Configura varias burn alerts con ventanas distintas (corta en horas, larga en días) y
  actúa si **cualquiera** salta; calibra la urgencia por anticipación: agotamiento en días →
  siguiente día hábil; en minutos/horas → despertar al on-call.
- `SHOULD` Recuerda el techo predictivo: por encima de ~99.95 % las burn alerts ya no previenen la
  violación, solo informan la degradación.
- `SHOULD` Usa SLOs **event-based** para proteger mejoras (ej. "% de interacciones más rápidas que un
  baseline") en vez de alarmas por umbral cuando la disponibilidad de dependencias es variable.
- `SHOULD` Al agotar el presupuesto, congela features y prioriza estabilidad — y diseña las alertas
  para no llegar nunca ahí.

### 6.4 El punto ciego del backend de observabilidad

- `MUST` Dale al pipeline/backend de telemetría su **propio SLO de frescura** (patrón sintético: N
  mensajes conocidos por minuto, consultados cada pocos segundos; frescura objetivo de segundos). Tu
  servicio puede estar sano mientras tú te quedas ciego — y ningún SLO de servicio lo registraría.
- `AVOID` Depender de sondas sintéticas como única detección: solo ven caídas totales, nunca el 2 %
  que falla ni la degradación gradual.

---

## 7. Muestreo: barato y suficientemente preciso

### 7.1 Cuándo y cómo decidir

- `MUST` Pasada cierta escala, muestrea en lugar de preagregar: el evento muestreado conserva TODAS
  sus dimensiones; la métrica no conserva ninguna.
- `SHOULD` Observa la forma de tu tráfico antes de elegir estrategia: homepage (~90 % de solicitudes
  casi idénticas) tolera muestreo agresivo; un backend tras caché (cada solicitud única) no.
- `MUST` Elige el momento de la decisión según los campos que la determinan: **cabeza** (al iniciar)
  para campos estáticos (endpoint, `customer_id`) propagando decisión y tasa; **cola** (al terminar)
  para campos dinámicos (status, latencia); **búfer en el collector** si necesitas trazas completas
  con criterios de cola.

| Estrategia | Cómo | Úsala cuando |
|------------|------|--------------|
| Probabilidad constante | 1 de cada N | Tráfico homogéneo, errores no prioritarios |
| Tasa objetivo autoajustable | `tasa = req_último_min / (60 × eventos_obj_por_seg)` | Quieres volumen (y costo) plano y predecible |
| Por contenido (clave) | Tasas distintas por `SampleKey` (error, shard, bucket de latencia, tipo de cliente) | Los atípicos importan más que la base |
| Cabeza + propagación | Decisión en el span raíz, propagada por header | Trazas completas, campos estáticos |
| Cola / búfer | Decisión al cerrar (o en el collector con búfer) | Decidir por status/latencia; trazas completas ⇒ collector |

### 7.2 Reglas

- `MUST` La tasa viaja **dentro** del evento (`sample_rate` = cuántos representa). Sin ella, nadie
  puede reponderar y todos los totales mienten.
- `MUST` Repondera al analizar: conteos y sumas × `sample_rate`; percentiles y medianas
  descomponiendo cada evento en los N que representa. (La mediana ingenua de
  `[{1,×5},{3,×2},{7,×9}]` da 3; la correcta da **7**.)
- `MUST` Muestreo **consistente** para trazas: el ID de muestreo se genera una vez en la raíz y se
  propaga; nunca "cada servicio tira su dado" (produce trazas con agujeros).
- `MUST` Nunca "muestrear cada error" sin cuota propia: en un incidente los errores dejan de ser
  raros y la avalancha satura tu análisis justo cuando más lo necesitas. Cuota garantizada para
  atípicos + presupuesto separado para el resto.
- `SHOULD` Muestrea atípicos (error, latencia alta) conservando muchos más eventos que la base
  exitosa (ej. 1-de-5 vs 1-de-1000); y a un hijo ruidoso (Redis) muestréalo más agresivamente que a
  sus padres (ej. hijo 1-de-1000, padres 1-de-10) — el muestreo consistente garantiza que aun con
  tasas distintas nunca queden hijos huérfanos.
- `SHOULD` Expón la tasa como configuración ajustable en runtime (feature flag), y usa el centinela
  `-1.0` para "nunca registrar" (keepalives, health checks).
- `SHOULD` Usa librerías existentes (`dynsampler-go`, capacidades de OTel, tail_sampling del
  Collector) en vez de reimplementar — pero entiende la lógica para configurarla.
- `AVOID` Muestrear eventos con error a la misma tasa que los éxitos, o descartar el 100 % de algo
  "ruidoso" sin dejar rastro reponderable.

---

## 8. Pipeline de telemetría y almacén

- `SHOULD` Interpón un pipeline (**receptor → búfer → procesador → exportador**, encadenables) entre
  apps y backends: cambias el destino sin tocar la aplicación.
- `AVOID` Construir software de pipeline propio hoy: usa OpenTelemetry Collector, Fluent Bit o
  Vector (veredicto de los propios constructores: "hoy no es rentable"). Para casos únicos, escribe
  extensiones del Collector (un poco de Go) y arma tu distro con el **OCB**.
- `MUST` Búfer entre apps y backends (Kafka/Kinesis; retención horas-días) + búfer local acotado en
  el agente: la telemetría tiene picos en cascada exactamente cuando algo falla.
- `MUST` Censura PII/tokens/parámetros sensibles **en el pipeline**, antes del backend (el backfill
  de limpieza posterior es lo más caro que existe); enruta datos sensibles solo a destinos
  restringidos.
- `MUST` Valida calidad de datos: campos esperados con tipos esperados; timestamps ni muy viejos ni
  futuros (reemplázalos por la marca de ingesta o desvíalos).
- `MUST` En colas y bajo presión, prioriza la telemetría **reciente**; backfill de lo atrasado después.
- `MUST` Monitorea el pipeline mismo: errores, exactitud punta a punta (que lo descartado sea SOLO
  lo que debía descartarse) y su SLO de frescura (§6.4).
- `SHOULD` Enriquece en el pipeline con lo que la app no ve (región, metadatos del contenedor,
  IP→hostname, geo); detecta y corta ahí las explosiones de cardinalidad en métricas.
- `SHOULD` Adopción por fases en entornos *brownfield*; usa **dual-send** (a blob barato + a la DB de
  telemetría) al migrar o cambiar reglas de sampling, para comparar viejo vs. nuevo con confianza.
- `SHOULD` Dos horizontes de acceso: tiempo real (segundos de latencia, retención corta, triaje) y
  almacén analítico (horas de latencia, años de retención, tendencias). Los eventos crudos de
  debugging son efímeros por diseño.

**Exigencias al backend/almacén (lo compres o lo construyas):** resultados en **segundos** (la
"prueba del café": si da tiempo de ir por café, no sirve para producción); cualquier campo consultable
sin preagregación; ninguna dimensión privilegiada salvo el tiempo; frescura de segundos; alta
cardinalidad y dimensionalidad sin castigo; datos crudos (nada de rollups que destruyan el desglose).
Los almacenes columnares particionados por tiempo (Retriever de Honeycomb, ClickHouse) son el patrón
de referencia. Desconfía de precios que penalizan la curiosidad (por consulta/usuario/host): si la
cultura prende, las consultas crecen exponencialmente.

---

## 9. Depurar con datos: el ciclo de análisis central

El método que reemplaza a la intuición del veterano:

1. **Vista general**: ¿qué disparó la investigación (alerta, queja de cliente)?
2. **Verifica el cambio**: ¿hay un quiebre real en la curva de rendimiento?
3. **Busca dimensiones que expliquen**: muestras de filas buscando atípicos, `GROUP BY` de campos
   comunes (`status_code`, ruta), filtrar valores sospechosos.
4. **Aísla y repite**: filtra esa área como nuevo punto de partida, hasta la causa.

- `MUST` Depura desde primeros principios: hipótesis → validar/refutar con datos, tratando cada
  incidente como nuevo. Sin corazonadas, sin "donde estuvo la última vez".
- `SHOULD` Automatiza la fuerza bruta del paso 3 (estilo BubbleUp): comparar TODAS las dimensiones
  dentro del área anómala vs. la línea base y ranquear por diferencia. La máquina detecta patrones;
  el humano interpreta si son buenos o malos. No delegues el juicio a "AIOps".
- `SHOULD` Aplica el **test del ingeniero nuevo**: cualquiera del equipo (no solo el veterano) debe
  poder seguir los datos hasta la causa de un incidente que nunca vio. Si el mejor depurador es
  siempre el más antiguo, tienes intuición institucionalizada, no observabilidad.
- `SHOULD` Documenta orientación por servicio (dueño, on-call, dependencias, enlaces a consultas
  útiles) — eso sí envejece bien.
- `AVOID` Runbooks exhaustivos de causas raíz y dashboards custom por cada incidente: los fallos
  novedosos no se repiten; la documentación errónea es peor que la ausente.

### 9.1 Agentes de IA para observabilidad

- `SHOULD` Usa agentes de IA (LLM + tools que consultan telemetría) para acelerar la investigación en
  tres casos probados: **respuesta a incidentes**, **explicar errores** y **mejorar la calidad de la
  instrumentación**.
- `MUST` Dales a los agentes un **modelo mental explícito y legible por máquina** del sistema
  (topología de servicios, convenciones de nombres, issues conocidos): sin él, el agente "no sabe lo
  que no sabe", confunde correlación con causalidad y alucina campos.
- `SHOULD` Piensa en las personas agénticas: *copiloto* (asiste al humano), *comandante* (ejecuta bajo
  supervisión) y *cuidador* (vigila en background). La telemetría de alta calidad y el contexto rico
  son la condición para que cualquiera funcione.
- `AVOID` Esperar que un agente encuentre la aguja escaneando todo el pajar sin contexto: en
  producción "las cosas raras son la norma".

---

## 10. Observabilidad de IA (LLMs y features generativas)

La observabilidad tradicional no basta para features con LLMs: **opacidad** (no hay debugger, ni
repetibilidad garantizada), **avance vertiginoso** y **malos proxies de UX** (una respuesta rápida
pero incorrecta no sirve). La clave es un **flywheel de aprendizaje**: telemetría de producción → evals
→ mejoras al *harness* → aparecen en la telemetría.

- `MUST` Instrumenta con las **OTel GenAI semantic conventions** (input/output token count, model
  name, sampling params, system prompt) y modela también **las conexiones entre la parte IA y el resto
  del sistema** (auth, DBs, tools, APIs).
- `MUST` **Deriva el costo por request a query-time** (por modelo × tokens): no existe una key
  estándar de costo; hazlo consultable para comparar prompts/modelos.
- `SHOULD` Decide la visibilidad de inputs/outputs con criterio de privacidad: guarda un **link al
  system of record** en vez del contenido crudo cuando haya PII o payloads grandes.
- `SHOULD` Captura **feedback de usuario** (thumbs up/down, retry) como atributos: es señal de UX y
  fuente de nuevos evals.
- `SHOULD` Usa **SLOs** (no alertas por umbral) sobre proveedores upstream (rate limits, timeouts,
  cache hit rate) y "promueve" trazas de producción a **evals** (golden dataset + LLM-as-a-Judge +
  revisión manual).
- `SHOULD` Corrige errores del modelo primero con **código en el harness** (validación, coerción)
  cuando sea barato, además del prompt. *Debes probar en producción: ningún set de tests cubre todos
  los inputs.*
- Detalle de la *feature* de IA (evaluación, prompts, RAG, guardrails, inferencia): `AI-ENGINEERING.md`.
  Observabilidad del *agente* (razonamiento, HITL, autonomía): `AGENT-ENGINEERING.md` §10. Telemetría
  de un MCP server: `MCP-ENGINEERING.md` §10.

---

## 11. Feedback loops y "test in prod"

La entrega de software es un **sistema sociotécnico** de feedback loops. En la mayoría de las
organizaciones, de los dos loops principales **solo uno incluye producción**:

- **Loop de desarrollo** (build → tests → merge → seguir): rápido pero se detiene en el merge. *Lo que
  aprendes cuando pasan todos los tests es… que pasan todos los tests.*
- **Loop operacional** (alerta → investigar → arreglar): el único que incluye producción, pero por
  umbral, laggy y reactivo; orientado a bugs, no a entender el producto.
- **El loop faltante**: conectar *"escribí esto"* con *"veo y entiendo qué hizo en producción"*.

- `MUST` Cierra el loop faltante: rastrea la **intención del developer** desde el código hasta
  producción y valídala con **datos precisos**. "Sin spike de errores" no es preciso; preciso es *"la
  tasa de compactación subió 5% y el footprint bajó 30% para requests con este `build_id`, combinación
  de flags y usuarios de prueba en el canary de 10% vs. baseline de 90%"*.
- `SHOULD` Ancla la observabilidad en **señales centradas en el usuario y el negocio** (ej. *time to
  first token*, *costo por interacción*, tasa de completado de un flujo), medidas donde el usuario las
  percibe y **atadas a telemetría de bajo nivel** que responde el *por qué* en tiempo real.
- `SHOULD` Habilita el **flywheel de prácticas** de "test in prod": instrumentación + **feature flags**
  + entrega progresiva/canaries + **rollbacks automáticos**. Desacopla lanzamiento de despliegue.
- `AVOID` Que el loop operacional sea la única (o principal) forma en que los developers aprenden de su
  código: es "acumular riesgo a la velocidad de la IA".

---

## 12. Prácticas de equipo, ODD y madurez

### 12.1 Desarrollo impulsado por la observabilidad (ODD)

- `MUST` Ningún PR se fusiona sin su instrumentación, igual que no se fusiona sin pruebas. La
  revisión verifica estándares de observabilidad, de seguridad y necesidades del negocio.
- `MUST` Quien fusiona **observa su código en producción durante los 30-60 min post-deploy**; una
  alerta en esa ventana va a esa persona. Depurar con la intención fresca es órdenes de magnitud más
  barato.
- `SHOULD` Mide y reduce `commit→deploy`: es la métrica de salud del equipo; velocidad y calidad
  suben juntas (*Accelerate* / DORA).
- `SHOULD` Un deploy = un conjunto coherente de cambios de un ingeniero. Agrupar cambios de días es
  la causa nº 1 de reversiones de horas.
- `AVOID` "Merge y cruzar los dedos", y también revertir por instinto al primer síntoma sin
  investigar con flags y datos.
- `AVOID` Telemetría línea-por-línea estilo debug exhaustivo: la observabilidad opera a nivel de
  sistema. El *dónde* lo da la observabilidad (telescopio); la línea exacta, el debugger local
  (microscopio).

### 12.2 Adopción

- `SHOULD` Empieza por el problema más doloroso que nadie ha podido resolver — no por el servicio
  pequeño "de bajo riesgo" (todo el esfuerzo, cero beneficio visible) — y comparte el éxito.
- `SHOULD` Pavimenta el camino (*paved path*): que el camino correcto sea el de menor resistencia.
  Lleva la observabilidad a donde la gente ya está (dashboards, alertas — la lección de Dapper: un
  solo link duplicó el uso). Haz la adopción **segura** con dual-send como fallback del sampling.
- `SHOULD` Reaprovecha lo existente: bifurca los logs a un segundo destino, corre OTel en paralelo
  al APM, recrea los dashboards útiles como consultas guardadas.
- `SHOULD` Planifica el **último empujón**: la adopción iterativa se estanca en ½-⅔ de cobertura; el
  resto exige cronograma y esfuerzo explícito (semana de instrumentación, hackathon).
- `SHOULD` Generaliza la instrumentación en librerías internas reutilizables (**no envuelvas la API de
  OTel**; extiéndela) que resuelvan la **propagación de contexto** y permitan cambiar el backend sin
  tocar los call-sites.

### 12.3 Casos de uso especiales

- `SHOULD` **CI/CD como producción**: instruméntalo con OTel (workflow=traza, job/step=span, con
  `repo`+`branch`), define SLOs de latencia end-to-end y flake rate, y optimiza sobre el critical path.
  Es el mejor punto de entrada de bajo volumen para practicar observabilidad.
- `SHOULD` **Mobile/frontend**: adopta **observabilidad enfocada en el usuario** (modela intención +
  resultado y lígalos a telemetría de UX/performance); prefiere **almacenamiento local + control
  plane** para no perder datos en condiciones adversas; invierte el debugging (impacto en el usuario
  primero, causa técnica después).
- `SHOULD` **Performance engineering**: usa la **misma telemetría** que para confiabilidad; combina
  **trazas + profiling** (una sola no basta), trabaja con baseline y cuantificación de impacto
  (latencia/datos/dólares/CO₂), y optimiza fleet-wide antes que micro-optimizar.

### 12.4 Madurez

- `SHOULD` Audita con las **5 capacidades del OMM**: (1) responder a fallos con resiliencia, (2)
  entregar código de calidad, (3) gestionar complejidad y deuda, (4) lanzar con cadencia predecible,
  (5) comprender al usuario. Identifica la capacidad más roja que impacte al negocio, asigna dueños
  y patrocinio.
- `AVOID` Medir el éxito por "menos incidentes": al adoptar observabilidad los incidentes
  **visibles** suben — eso es progreso, no regresión.

---

## 13. Gobernanza: costo vs. inversión, build vs. buy, vendors

Para líderes técnicos: la observabilidad fija el **piso de la velocidad de aprendizaje** de la
organización. Estas reglas evitan "pagar precios de observabilidad por resultados de monitoreo".

### 13.1 Centro de costo vs. inversión

- `MUST` Gestiona cada loop con el modelo correcto: la observabilidad **operacional/de infra** es
  centro de costo (optimiza gasto); la de **aprendizaje del developer** es **inversión estratégica**
  (evaluada por lo que produce). Reporta la de producto a **ingeniería** (VP Eng/CTO), no a IT/ops, y
  córrela como **plataforma** (developers = clientes).
- `MUST` Mide si la inversión funciona con **indicadores concretos**: operacional → ↓ tiempo a causas
  conocidas, ↑ calidad de alertas, ↓ escalamientos, recuperación más rápida; developer → responder
  preguntas novedosas, ↑ frecuencia de deploy con ↓ batch, más gente depurando sola, ↑ issues hallados
  internamente vs. por clientes.
- `SHOULD` Gestiona el gasto con **tiers de telemetría** (critical path → trazas ricas + tail sampling
  alto; servicios estables → health checks), **feature flags de verbosidad** (sin redeploy) y
  **atribución por equipo** (etiqueta cada request con su costo; presupuesto por equipo).
- `AVOID` Recortar ingesta a ciegas bajo presión de costo: crea puntos ciegos → más riesgo y más costo
  a largo plazo. Diagnostica antes de recortar.

### 13.2 Build vs. buy vs. open source

- `MUST` Decide con el **2×2 de ubicuidad × valor de negocio**: *build* (alto valor + bespoke), *buy*
  (commodity + bajo valor — la mayoría de las plataformas de observabilidad), *should-not-exist*
  (bespoke + bajo valor), *decide* (commodity + alto valor). Deja el cuadrante "decide" a **ingenieros
  staff+**, no al decreto gerencial.
- `MUST` La observabilidad **no es como otro software**: requiere **alta disponibilidad desacoplada**
  de tus propias dependencias (si corre en tu mismo cloud/región, caerá justo cuando la necesitas).
- `SHOULD` Para la mayoría: **compra la plataforma, construye la capa de integración** (instrumentación
  + pipeline + librerías idiomáticas) con OTel nativo. Contabiliza los **costos ocultos** de "lo
  gratis" (mantenimiento, oportunidad, headcount).
- `MUST` Usa **OTel nativo por defecto** y delega lo específico del vendor a distros/exporters, para
  migrar sin reinstrumentar. Si un vendor te desalienta de OTel, busca lock-in.

### 13.3 Vendor engineering

- `SHOULD` Trata la compra con el rigor de un sistema de producción: mapea stakeholders (ingeniería,
  finanzas, seguridad, procurement), construye **credibilidad/confianza** (internos) y **reciprocidad**
  (externos), diseña **POCs que ejerciten tus problemas más difíciles** con criterios de éxito
  definidos **de antemano**, y codifica la relación en **SLAs**. No terminas hasta **decomisionar** lo
  viejo, y planea desde ya la próxima transición.

---

## 14. Mapeo a stacks comunes

El estándar es agnóstico; estos son los equivalentes al aterrizarlo:

| Concepto | OTel semconv | DataDog | Notas pino/NestJS |
|----------|--------------|---------|-------------------|
| Severidad | `SeverityText` | `status` | `customLogLevel` por status HTTP |
| Servicio/entorno/versión | `service.name/…` (resource) | `service`, `env`, `version` (Unified Service Tagging) | Base del logger desde `DD_*` o env vars |
| Traza | `trace_id`, `span_id` (W3C) | `dd.trace_id`, `dd.span_id` | Emite ambos si conviven dd-trace y OTel |
| Ruta normalizada | `http.route` | `http.route` | `req.route?.path` de Express al cierre |
| Duración | `duration` (ns en spans) | `duration` (ns) | Convierte `responseTime` ms → ns |
| Error | `exception.type/message/stacktrace` | `error.kind/message/stack` | Pasa el `Error` real, nunca `String(err)` |
| Usuario | `enduser.id` | `usr.id/name/email` | Vía contexto de request (ALS) |
| IA generativa | `gen_ai.*` (GenAI semconv) | atributos LLM del vendor | Tokens, modelo, prompt; costo derivado a query-time |
| Evento canónico | span raíz + atributos | línea única "request completed" enriquecida | ALS acumula → volcado al responder |

- `SHOULD` Al usar un vendor, usa **sus** atributos reservados para que su UI los reconozca, pero
  mantén los call-sites agnósticos (wrapper propio) y documenta el mapeo hacia OTel semconv: ese
  documento ES tu plan de salida.

---

## 15. Anti-patrones (resumen)

- `AVOID` Muchos logs narrativos sueltos por request en vez de un evento canónico ancho.
- `AVOID` Preagregar en escritura; alta cardinalidad como etiquetas de métricas.
- `AVOID` Nombres de campo dinámicos/de un solo uso; envolver (no extender) la API de OTel.
- `AVOID` Alertas por causas (CPU/memoria/disco) y umbrales estáticos; acumular alertas post-mortem;
  tolerar alertas ruidosas.
- `AVOID` SLIs por buckets de tiempo con SLOs estrictos; error budget en ventana de calendario;
  extrapolación lineal.
- `AVOID` "Muestrear cada error" sin cuota; muestreo sin `sample_rate` en el evento; decisiones de
  muestreo independientes por servicio.
- `AVOID` Instrumentación 100 % automática o 100 % propietaria (lock-in).
- `AVOID` Dashboards estáticos y runbooks de causas como herramienta de descubrimiento; depurar por
  corazonada; AIOps como juez; agentes de IA sin modelo mental explícito del sistema.
- `AVOID` PII/secrets en telemetría; payloads completos en logs.
- `AVOID` Tres o cuatro sistemas de telemetría inconexos donde el ingeniero carga el contexto entre
  ellos; que el loop operacional sea la única forma de aprender de producción.
- `AVOID` Tratar la observabilidad como proyecto con fecha de fin, como compra de herramienta, o como
  centro de costo cuando observa código que genera ingresos.

---

## 16. Checklists

**Evento listo**
- [ ] Un evento canónico por unidad de trabajo (no N logs sueltos)
- [ ] Campos base completos (§3.2): correlación, deploy, request, negocio, resultado, `sample_rate`
- [ ] `http.route` normalizada además de la URL cruda
- [ ] `build_id`/`commit_hash` y feature flags presentes
- [ ] Ids de negocio de alta cardinalidad incluidos
- [ ] Redacción de PII/secrets verificada (incluidos objetos anidados); hash one-way si aplica
- [ ] Nombres según convención/semconv compartida; cero claves dinámicas

**Trazas listas**
- [ ] 5 campos obligatorios en cada span
- [ ] Propagación W3C `traceparent` en toda llamada saliente
- [ ] Spans propios solo donde sean interesantes/agregables; el resto como desgloses de tiempo
- [ ] Spans enriquecidos con contexto de negocio

**SLOs y alertas listos**
- [ ] SLIs por evento con criterio bueno/malo explícito
- [ ] SLO en ventana deslizante de 30 días, más estricto que el SLA
- [ ] Burn alerts proporcionales con factor de 4 y ventanas múltiples
- [ ] Cero alertas que no cumplan los 2 criterios (§6.1)
- [ ] SLO de frescura del pipeline de telemetría

**IA lista (si aplica)**
- [ ] OTel GenAI semconv (tokens, modelo, prompt) + costo por request a query-time
- [ ] Feedback de usuario capturado; trazas promovibles a evals
- [ ] SLOs (no umbrales) sobre proveedores upstream
- [ ] Agentes de observabilidad con modelo mental explícito del sistema

**Producción lista**
- [ ] Muestreo con `sample_rate` y reponderación en el análisis
- [ ] Pipeline con búfer, censura de PII y validación de timestamps
- [ ] PR sin instrumentación = PR incompleto (revisión lo verifica)
- [ ] Ventana de observación post-deploy (30-60 min) asignada
- [ ] Loop del developer cerrado: intención validada en producción con datos precisos
- [ ] Test del ingeniero nuevo superado; TTD/TTR con línea base

**Gobernanza lista (si eres líder técnico)**
- [ ] ¿Cuál loop es el cuello de botella (dev u ops)? Alineado antes de comprar
- [ ] Observabilidad de producto gestionada como inversión, bajo ingeniería, como plataforma
- [ ] Gasto con tiers, feature flags de verbosidad y atribución por equipo
- [ ] Decisión build/buy con el 2×2; OTel nativo para evitar lock-in

---

## 17. Plantilla ADR

```markdown
# ADR-NNNN — <título de la decisión de observabilidad>

## Contexto
<qué problema de telemetría/alertas/costo/feedback loop se está resolviendo>

## Decisión
<qué se decidió: estrategia de muestreo, esquema de campos, ventana de SLO, build vs buy...>

## Alternativas descartadas
<qué otras opciones se evaluaron y por qué no>

## Consecuencias
<qué se gana, qué se pierde, qué costo/volumen implica>

## Cómo se evalúa
<qué señal o métrica dirá si la decisión fue correcta>
```

---

> Mantén este documento como fuente de verdad. Si una regla estorba, **no la ignores en silencio**:
> registra un ADR justificando la excepción.
