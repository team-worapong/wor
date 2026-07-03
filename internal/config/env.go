package config

import (
	"os"
	"strings"
)

type Env map[string]string

func EnvironmentVariables() []string {
	return []string{
		EnvConfig,
		EnvHome,
		EnvDataDir,
		EnvCacheDir,
		EnvOutput,
		EnvDebug,
	}
}

func FromOSEnv() Env {
	env := make(Env)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func (e Env) Get(key string) string {
	if e == nil {
		return ""
	}
	return e[key]
}

func (e Env) ValueOr(key, fallback string) string {
	if value := strings.TrimSpace(e.Get(key)); value != "" {
		return value
	}
	return fallback
}
