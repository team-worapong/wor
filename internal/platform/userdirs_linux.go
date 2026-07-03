//go:build linux

package platform

import (
	"os"
	"path/filepath"
)

func userDataDir(appName string) (string, error) {
	if dataHome := os.Getenv("XDG_DATA_HOME"); dataHome != "" {
		return filepath.Join(dataHome, appName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}
