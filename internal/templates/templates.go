// Package templates embeds wor's nginx/apache virtual-host templates,
// ported unchanged (in content) from templates/nginx, templates/apache,
// and templates/webserver/{nginx,apache} in the shell CLI. Only the
// rendering mechanism changed (see internal/render); the template
// syntax and variable names are identical so existing operational
// knowledge of wor's generated configs carries over.
package templates

import (
	"embed"
	"fmt"
	"path"
)

//go:embed assets
var assetsFS embed.FS

// Get returns the raw template body for "<group>/<name>", e.g.
// Get("nginx", "static.conf") or Get("webserver/apache", "http.conf").
func Get(group, name string) (string, error) {
	p := path.Join("assets", group, name)
	data, err := assetsFS.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("template not found: %s/%s: %w", group, name, err)
	}
	return string(data), nil
}
