package keeper

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/metric"
)

// Clasificacion dice cómo puede salir un atributo que el contrato declara.
type Clasificacion uint8

const (
	// Operativo no contiene datos personales (estados, conteos, catálogos, ids
	// internos): sale tal cual.
	Operativo Clasificacion = iota
	// Identificador reconoce a una persona (CURP, RFC, correo): sale solo como
	// hash h1:… cuando hay pepper, y censurado cuando no.
	Identificador
)

// ModoContrato decide qué pasa con un atributo que el contrato no declara.
type ModoContrato uint8

const (
	// Reportar lo deja pasar y lo registra como violación. Sirve para adoptar el
	// contrato en un servicio que ya emite atributos sin declarar, sin perder
	// información mientras se ordena.
	Reportar ModoContrato = iota
	// Descartar lo quita antes de exportar y lo registra como violación.
	Descartar
)

// Contrato declara qué atributos puede emitir un servicio. Lo normal es generarlo
// desde un registro de convenciones semánticas, no escribirlo a mano. Los atributos
// que emite el propio SDK (request_id, http.*, exception.*…) están siempre permitidos.
type Contrato struct {
	Atributos map[string]Clasificacion
	Modo      ModoContrato
}

// WithContrato activa el modo estricto: los atributos no declarados se reportan o se
// descartan según el modo, en logs, trazas y métricas. Sin esta opción no se filtra
// nada, como antes.
func WithContrato(c Contrato) Option {
	return func(cfg *config) { cfg.contrato = &c }
}

// Violacion es un atributo fuera del contrato que el servicio intentó emitir.
type Violacion struct {
	Clave     string
	Senal     string // logs | trazas | metricas
	Descartes int64  // veces que se quitó antes de exportar
	Reportes  int64  // veces que salió igual por estar en modo Reportar
}

const (
	senalLogs     = "logs"
	senalTrazas   = "trazas"
	senalMetricas = "metricas"
)

// propiasDelSDK son las claves que el SDK y keeperfiber emiten por su cuenta. Nunca
// cuentan como violación: el servicio no las controla y el SDK ya las cuida.
var propiasDelSDK = map[string]struct{}{
	"request_id": {}, "sample_rate": {}, "duration_ms": {}, "panic": {},
	"client.address": {}, "client.browser": {}, "client.os": {}, "client.device.type": {},
	"user_agent.original": {},
	"http.request.method": {}, "http.response.status_code": {}, "http.route": {}, "url.path": {},
	"exception.type": {}, "exception.message": {}, "exception.stacktrace": {}, "exception.escaped": {},
	"enduser.id": {}, "tenant.id": {}, "business.success": {}, "error.kind": {}, "error.message": {},
	"trace_id": {}, "span_id": {},
	"keeper.contrato.senal": {}, "keeper.contrato.accion": {},
}

// contratoActivo es el contrato en uso más su contabilidad de violaciones.
type contratoActivo struct {
	atributos map[string]Clasificacion
	modo      ModoContrato

	mu         sync.Mutex
	violadas   map[claveSenal]*Violacion
	avisadas   int
	avisoLimit int
}

type claveSenal struct{ clave, senal string }

var (
	contratoMu     sync.RWMutex
	contratoGlobal *contratoActivo
)

func nuevoContratoActivo(c Contrato) *contratoActivo {
	atributos := make(map[string]Clasificacion, len(c.Atributos))
	for k, v := range c.Atributos {
		atributos[k] = v
	}
	return &contratoActivo{atributos: atributos, modo: c.Modo, violadas: map[claveSenal]*Violacion{}, avisoLimit: 200}
}

func setContrato(c *contratoActivo) {
	contratoMu.Lock()
	contratoGlobal = c
	contratoMu.Unlock()
}

func contratoEnUso() *contratoActivo {
	contratoMu.RLock()
	defer contratoMu.RUnlock()
	return contratoGlobal
}

// admite dice si la clave puede salir y la registra como violación si no está
// declarada. Con el contrato en modo Reportar, lo no declarado sale igual.
func (c *contratoActivo) admite(clave, senal string) bool {
	if _, ok := c.atributos[clave]; ok {
		return true
	}
	if _, ok := propiasDelSDK[clave]; ok {
		return true
	}
	c.registrar(clave, senal)
	return c.modo == Reportar
}

func (c *contratoActivo) esIdentificador(clave string) bool {
	return c.atributos[clave] == Identificador && c.declarada(clave)
}

func (c *contratoActivo) declarada(clave string) bool {
	_, ok := c.atributos[clave]
	return ok
}

func (c *contratoActivo) registrar(clave, senal string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := claveSenal{clave, senal}
	v, ok := c.violadas[k]
	if !ok {
		v = &Violacion{Clave: clave, Senal: senal}
		c.violadas[k] = v
		// Un aviso por clave, con tope: el nombre de la clave va a stderr y no a una
		// métrica, para no crear cardinalidad con un error del propio servicio.
		if c.avisadas < c.avisoLimit {
			c.avisadas++
			fmt.Fprintf(os.Stderr, "keeper: atributo fuera del contrato %q en %s (%s)\n", clave, senal, c.accion())
		}
	}
	if c.modo == Descartar {
		v.Descartes++
	} else {
		v.Reportes++
	}
}

func (c *contratoActivo) accion() string {
	if c.modo == Descartar {
		return "descartado"
	}
	return "reportado"
}

func (c *contratoActivo) copiaViolaciones() []Violacion {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Violacion, 0, len(c.violadas))
	for _, v := range c.violadas {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Clave != out[j].Clave {
			return out[i].Clave < out[j].Clave
		}
		return out[i].Senal < out[j].Senal
	})
	return out
}

// Violaciones devuelve los atributos fuera del contrato vistos desde el arranque (o
// desde ReiniciarViolaciones). Vacío si no hay contrato activo. Es lo que una prueba
// de contrato revisa para fallar con el nombre de la clave.
func Violaciones() []Violacion {
	c := contratoEnUso()
	if c == nil {
		return nil
	}
	return c.copiaViolaciones()
}

// ReiniciarViolaciones borra la contabilidad de violaciones. Pensado para pruebas.
func ReiniciarViolaciones() {
	if c := contratoEnUso(); c != nil {
		c.mu.Lock()
		c.violadas = map[claveSenal]*Violacion{}
		c.avisadas = 0
		c.mu.Unlock()
	}
}

// registrarMetricaDeViolaciones expone el conteo como contador observable, sin la
// clave: en Keeper alimenta la alerta de "algo salió del contrato". Se lee al
// recolectar, fuera del camino de cada medición.
func registrarMetricaDeViolaciones(m metric.Meter, c *contratoActivo) error {
	_, err := m.Int64ObservableCounter("keeper.contrato.violaciones",
		metric.WithDescription("Atributos fuera del contrato de telemetría, por señal y acción"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			totales := map[[2]string]int64{}
			for _, v := range c.copiaViolaciones() {
				if v.Descartes > 0 {
					totales[[2]string{v.Senal, "descartado"}] += v.Descartes
				}
				if v.Reportes > 0 {
					totales[[2]string{v.Senal, "reportado"}] += v.Reportes
				}
			}
			for k, n := range totales {
				o.Observe(n, metric.WithAttributes(attrString("keeper.contrato.senal", k[0]), attrString("keeper.contrato.accion", k[1])))
			}
			return nil
		}))
	return err
}

// clavesIdentificador devuelve, en minúsculas, las claves que el contrato clasifica
// como identificador: se suman a las de censura y hash del SDK.
func (c *contratoActivo) clavesIdentificador() []string {
	var out []string
	for k, v := range c.atributos {
		if v == Identificador {
			out = append(out, strings.ToLower(k))
		}
	}
	sort.Strings(out)
	return out
}
