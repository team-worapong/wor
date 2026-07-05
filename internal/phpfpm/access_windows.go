//go:build windows

package phpfpm

import "fmt"

// GrantGroupAccess is unsupported on Windows -- per-service php-fpm
// pools are Linux/macOS only (see phpfpm.go's package doc).
func GrantGroupAccess(docRoot, poolUser string) (string, error) {
	return "", fmt.Errorf("per-service php-fpm pools are not supported on Windows")
}
