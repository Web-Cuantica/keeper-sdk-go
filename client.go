package keeper

import "github.com/mileusna/useragent"

// Client describe el origen de un request (IP + dispositivo) para enriquecer los
// logs: desde dónde y con qué se hizo la petición.
type Client struct {
	Address    string // IP del cliente
	UserAgent  string // User-Agent crudo
	Browser    string // navegador (p.ej. "Chrome")
	OS         string // sistema operativo (p.ej. "Windows")
	DeviceType string // desktop | mobile | tablet | bot (vacío si no se pudo inferir)
}

// ParseClient arma un Client a partir de la IP y el User-Agent. Si el UA viene
// vacío, solo se conserva la dirección.
func ParseClient(ip, ua string) Client {
	c := Client{Address: ip, UserAgent: ua}
	if ua == "" {
		return c
	}
	p := useragent.Parse(ua)
	c.Browser = p.Name
	c.OS = p.OS
	switch {
	case p.Bot:
		c.DeviceType = "bot"
	case p.Tablet:
		c.DeviceType = "tablet"
	case p.Mobile:
		c.DeviceType = "mobile"
	case p.Desktop:
		c.DeviceType = "desktop"
	}
	return c
}
