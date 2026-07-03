//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

func userDataDir(appName string) (string, error) {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, appName), nil
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName), nil
}
