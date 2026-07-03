//go:build !darwin && !linux && !windows

package platform

import (
	"os"
	"path/filepath"
)

func userDataDir(appName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "."+appName), nil
}
