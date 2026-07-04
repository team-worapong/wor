// Package render implements wor's tiny "{{VAR}}" template substitution,
// porting lib/webserver.sh render_template()/render_template_capture().
// The shell version substituted every process environment variable into
// `{{KEY}}` placeholders by shelling out to Node; the Go version takes
// an explicit map instead, which is simpler, faster, and doesn't leak
// unrelated environment variables into generated config files.
package render

import "strings"

// Render replaces every "{{KEY}}" occurrence in src with vars["KEY"].
// Placeholders whose key is not present in vars are left untouched.
func Render(src string, vars map[string]string) string {
	if len(vars) == 0 {
		return src
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(src)
}
