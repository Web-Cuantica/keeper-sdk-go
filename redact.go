package keeper

// redactCensor es el valor con el que se reemplazan los atributos sensibles.
const redactCensor = "***"

// defaultRedactKeys son las claves de atributo censuradas por defecto (PII/secrets).
// Comparación case-insensitive. Se pueden añadir más con WithRedactKeys.
func defaultRedactKeys() map[string]struct{} {
	keys := []string{
		"authorization", "password", "passwd", "secret", "token",
		"api_key", "apikey", "access_token", "refresh_token",
		"vin", "email", "credit_card", "card_number", "cvv",
		"ssn", "curp", "rfc",
	}
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}
