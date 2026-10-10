package http

import (
	"fmt"
	"net/url"
	"strings"
)

func laborContentDisposition(filename string) string {
	asciiName := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, filename)
	asciiName = strings.Trim(asciiName, "-")
	if asciiName == "" {
		asciiName = "download"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName, url.PathEscape(filename))
}
