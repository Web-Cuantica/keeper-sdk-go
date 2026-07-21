package keeper

import (
	"log/slog"
	"os"
	"strings"
)

// config es la configuración resuelta del SDK (opciones + entorno).
type config struct {
	service     string
	env         string
	version     string
	endpoint    string
	level       slog.Level
	levelSet    bool
	redactKeys  map[string]struct{}
	hashKeys    map[string]struct{} // PII hasheable (§3.4); vacío ⇒ defaultHashKeys
	hashPepper  string              // HMAC pepper; vacío ⇒ PII se censura con "***"
	sampleRatio *float64            // nil => usar entorno/def; ver resolveSampleRatio (§7)
	buildID     string              // build_id (§3.2): vacío => no se emite
	commitHash  string              // commit_hash (§3.2): vacío => no se emite
}

// Option configura el SDK. Las opciones tienen prioridad sobre las variables de entorno.
type Option func(*config)

// WithService fija el nombre del servicio (resource service.name).
func WithService(s string) Option {
	return func(c *config) {
		if s != "" {
			c.service = s
		}
	}
}

// WithEnv fija el ambiente (resource deployment.environment).
func WithEnv(e string) Option {
	return func(c *config) {
		if e != "" {
			c.env = e
		}
	}
}

// WithVersion fija la versión del servicio (resource service.version).
func WithVersion(v string) Option {
	return func(c *config) {
		if v != "" {
			c.version = v
		}
	}
}

// WithEndpoint fija el endpoint OTLP/HTTP de la plataforma Keeper (p. ej. http://host:4318).
func WithEndpoint(u string) Option {
	return func(c *config) {
		if u != "" {
			c.endpoint = u
		}
	}
}

// WithLevel fija el nivel mínimo de log (trace|debug|info|warn|error|fatal).
func WithLevel(l string) Option {
	return func(c *config) {
		c.level = parseLevel(l)
		c.levelSet = true
	}
}

// WithRedactKeys añade claves a censurar (se suman a las de PII/secrets por defecto).
func WithRedactKeys(keys ...string) Option {
	return func(c *config) {
		for _, k := range keys {
			c.redactKeys[strings.ToLower(k)] = struct{}{}
		}
	}
}

// WithHashPepper fija el pepper HMAC para hashes one-way de identificadores
// sensibles (§3.4 SHOULD). Debe ser el MISMO en todos los servicios de la
// organización para correlacionar. También: KEEPER_HASH_PEPPER.
// Sin pepper, las claves hasheables se censuran con "***" (no se emite el valor).
func WithHashPepper(pepper string) Option {
	return func(c *config) {
		c.hashPepper = pepper
	}
}

// WithHashKeys reemplaza el conjunto de claves que se hashean (en lugar de
// censurar) cuando hay pepper. Por defecto: email, curp, rfc, vin, ssn.
// Los secretos (password/token/…) no deben incluirse aquí.
func WithHashKeys(keys ...string) Option {
	return func(c *config) {
		m := make(map[string]struct{}, len(keys))
		for _, k := range keys {
			if k != "" {
				m[strings.ToLower(k)] = struct{}{}
			}
		}
		c.hashKeys = m
	}
}

// WithSampleRatio fija la proporción de muestreo de trazas en [0,1] (1 = sin muestreo,
// 0.1 = 1 de cada 10). Tiene prioridad sobre OTEL_TRACES_SAMPLER del entorno. El
// sample_rate resultante (1/ratio) se estampa en el span del request (§7).
func WithSampleRatio(r float64) Option {
	return func(c *config) {
		c.sampleRatio = &r
	}
}

// WithBuildID fija el build_id del artefacto desplegado (resource, §3.2).
func WithBuildID(id string) Option {
	return func(c *config) {
		if id != "" {
			c.buildID = id
		}
	}
}

// WithCommitHash fija el commit_hash desplegado (resource, §3.2): atribuye regresiones al deploy.
func WithCommitHash(h string) Option {
	return func(c *config) {
		if h != "" {
			c.commitHash = h
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveConfig arma la config a partir del entorno y aplica las opciones encima.
func resolveConfig(opts ...Option) config {
	c := config{
		service:    firstNonEmpty(os.Getenv("KEEPER_SERVICE_NAME"), os.Getenv("SERVICE_NAME"), "unknown-service"),
		env:        firstNonEmpty(os.Getenv("KEEPER_ENV"), os.Getenv("NODE_ENV"), "development"),
		version:    firstNonEmpty(os.Getenv("KEEPER_SERVICE_VERSION"), os.Getenv("APP_VERSION"), "0.0.0"),
		endpoint:   firstNonEmpty(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), "http://localhost:4318"),
		redactKeys: defaultRedactKeys(),
		hashKeys:   defaultHashKeys(),
		hashPepper: os.Getenv("KEEPER_HASH_PEPPER"),
		buildID:    os.Getenv("KEEPER_BUILD_ID"),
		commitHash: firstNonEmpty(os.Getenv("KEEPER_COMMIT_HASH"), os.Getenv("GIT_COMMIT"), os.Getenv("COMMIT_SHA")),
	}
	if env := os.Getenv("KEEPER_LOG_LEVEL"); env != "" {
		c.level = parseLevel(env)
		c.levelSet = true
	}
	for _, o := range opts {
		o(&c)
	}
	if !c.levelSet {
		c.level = defaultLevelForEnv(c.env)
	}
	return c
}

// parseLevel mapea el nivel del estándar a slog (trace->debug, fatal->error).
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace", "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "fatal":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// defaultLevelForEnv: dev->debug, resto->info.
func defaultLevelForEnv(env string) slog.Level {
	switch strings.ToLower(env) {
	case "local", "dev", "development":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
