package keeper

import (
	"strings"
	"unicode/utf8"
)

// SafeUTF8 devuelve s garantizando que sea UTF-8 válido, eliminando las secuencias
// de bytes inválidas.
//
// Es imprescindible antes de poner un valor de origen no confiable (rutas, User-Agent,
// query params) como atributo de span o log: el exportador OTLP RECHAZA el lote COMPLETO
// si un solo campo string contiene UTF-8 inválido ("string field contains invalid UTF-8"),
// y con él se pierden spans/logs legítimos del mismo batch. Un bot sondeando la IP pública
// con bytes crudos en la ruta bastaba para tumbar exports enteros.
func SafeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	// Elimina los bytes inválidos (no los sustituye por U+FFFD) para no ensuciar el
	// almacenamiento con caracteres de reemplazo.
	return strings.ToValidUTF8(s, "")
}
