package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type System struct {
	goos   string
	goarch string
}

func Current() System {
	return System{
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}
}

func (s System) OS() string {
	return s.goos
}

func (s System) Arch() string {
	return s.goarch
}

func (s System) String() string {
	return s.goos + "/" + s.goarch
}

func (s System) IsSupported() bool {
	return isSupportedOS(s.goos) && isSupportedArch(s.goarch)
}

func (s System) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (s System) CommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", err
		}
		return text, fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}

func (s System) UserConfigDir(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName), nil
}

func (s System) UserCacheDir(appName string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName), nil
}

func (s System) UserDataDir(appName string) (string, error) {
	return userDataDir(appName)
}

func isSupportedOS(goos string) bool {
	switch goos {
	case "linux", "darwin", "windows":
		return true
	default:
		return false
	}
}

func isSupportedArch(goarch string) bool {
	switch goarch {
	case "amd64", "arm64":
		return true
	default:
		return false
	}
}
