package keeper

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
)

// hashPrefix versiona el formato del hash one-way (§3.4 SHOULD). Permite rotar el
// algoritmo sin ambigüedad en consultas históricas.
const hashPrefix = "h1:"

var (
	hashMu           sync.Mutex
	hashPepperGlobal string
	hashKeysGlobal   = defaultHashKeys()
)

// defaultHashKeys son identificadores sensibles que, con pepper configurado, se
// reemplazan por un hash one-way en lugar de "***". Así se correlaciona sin
// exponer el dato (email/curp/rfc…). Los secretos (password/token/…) NUNCA van
// aquí: siempre se censuran.
func defaultHashKeys() map[string]struct{} {
	keys := []string{"email", "curp", "rfc", "vin", "ssn"}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}

func setHashConfig(pepper string, keys map[string]struct{}) {
	hashMu.Lock()
	hashPepperGlobal = pepper
	if keys != nil {
		hashKeysGlobal = keys
	}
	hashMu.Unlock()
}

func getHashPepper() string {
	hashMu.Lock()
	defer hashMu.Unlock()
	return hashPepperGlobal
}

func getHashKeys() map[string]struct{} {
	hashMu.Lock()
	defer hashMu.Unlock()
	return hashKeysGlobal
}

// normalizeID unifica el valor antes de hashear para que el mismo identificador
// produzca el mismo digest entre servicios (trim + minúsculas).
func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// HashID calcula un HMAC-SHA256 one-way del identificador con el pepper global
// (WithHashPepper / KEEPER_HASH_PEPPER). Devuelve "h1:<hex>" o "" si no hay
// pepper o el valor está vacío. Misma entrada + mismo pepper ⇒ mismo hash en
// todos los servicios de la organización (§3.4).
//
//	keeper.Annotate(ctx, slog.String("email", keeper.HashID(user.Email)))
//
// Preferible dejar que el redactHandler lo haga solo: con pepper configurado,
// las claves de defaultHashKeys se hashean automáticamente al emitir el log/span.
func HashID(value string) string {
	return hashIDWithPepper(getHashPepper(), value)
}

// hashIDWithPepper es la implementación pura (testeable sin globals).
func hashIDWithPepper(pepper, value string) string {
	if pepper == "" || value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(pepper))
	_, _ = mac.Write([]byte(normalizeID(value)))
	return hashPrefix + hex.EncodeToString(mac.Sum(nil))
}

// IsHashed reporta si s tiene el formato de hash Keeper (prefijo h1:).
func IsHashed(s string) bool {
	return strings.HasPrefix(s, hashPrefix) && len(s) > len(hashPrefix)
}
