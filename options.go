package keeper

import (
	"log/slog"
	"os"
	"strings"
)

// config es la configuración resuelta del SDK (opciones + entorno).
type config struct {
	service    string
	env        string
	version    string
	endpoint   string
	level      slog.Level
	levelSet   bool
	redactKeys map[string]struct{}
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
