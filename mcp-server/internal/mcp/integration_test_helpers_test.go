package mcp

import "encoding/base64"

func htmlDataURL(html string) string {
	return "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
}
