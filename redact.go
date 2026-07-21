package keeper

import (
	"log/slog"
	"strings"
	"sync"
)

// redactCensor es el valor con el que se reemplazan los atributos sensibles.
const redactCensor = "***"

var (
	redactMu         sync.Mutex
	redactKeysGlobal = defaultRedactKeys()
)

// setRedactKeys fija el conjunto de claves sensibles usado por RedactAttrs (lo llama Start).
func setRedactKeys(keys map[string]struct{}) {
	redactMu.Lock()
	redactKeysGlobal = keys
	redactMu.Unlock()
}

// RedactAttrs devuelve los atributos con las claves sensibles censuradas (o
// hasheadas si hay pepper y la clave está en hashKeys), de forma recursiva sobre
// grupos anidados. Úsalo antes de poner atributos de negocio en un span (los spans
// no pasan por el handler de redacción de logs) para no filtrar PII/secrets (§3.4).
func RedactAttrs(attrs []slog.Attr) []slog.Attr {
	redactMu.Lock()
	keys := redactKeysGlobal
	redactMu.Unlock()
	h := redactHandler{
		keys:     keys,
		hashKeys: getHashKeys(),
		pepper:   getHashPepper(),
	}
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.redact(a)
	}
	return out
}

// defaultRedactKeys son las claves de atributo censuradas por defecto (PII/secrets).
// Comparación case-insensitive y por SUBCADENA (ver matchRedact): mejor redactar de más
// que filtrar un secreto (OBSERVABILITY-ENGINEERING.md §3.4). Se pueden añadir más con
// WithRedactKeys.
//
// Contrato compartido con keeper-sdk-js: misma lista lógica de claves (secretos + PII del
// dominio) y misma semántica de match, para que ambos SDK se comporten igual entre servicios.
func defaultRedactKeys() map[string]struct{} {
	keys := []string{
		// Secretos y credenciales
		"authorization", "password", "passwd", "secret", "token",
		"api_key", "apikey", "access_token", "refresh_token",
		"cookie", "credential", "private_key",
		// PII del dominio
		"vin", "email", "credit_card", "card_number", "cvv",
		"ssn", "curp", "rfc",
	}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

// matchRedact indica si la clave debe censurarse: comparación case-insensitive por
// subcadena contra el conjunto de claves sensibles (que ya vienen en minúsculas).
func matchRedact(key string, keys map[string]struct{}) bool {
	lk := strings.ToLower(key)
	for needle := range keys {
		if strings.Contains(lk, needle) {
			return true
		}
	}
	return false
}
