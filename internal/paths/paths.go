package paths

import (
	"path/filepath"

	"github.com/team-worapong/wor/internal/platform"
)

type Paths struct {
	ConfigDir  string
	ConfigFile string
	HomeDir    string
	DataDir    string
	CacheDir   string
}

type Resolver struct {
	system  platform.System
	appName string
}

func NewResolver(system platform.System, appName string) Resolver {
	return Resolver{
		system:  system,
		appName: appName,
	}
}

func (r Resolver) Resolve() (Paths, error) {
	configDir, err := r.system.UserConfigDir(r.appName)
	if err != nil {
		return Paths{}, err
	}

	dataDir, err := r.system.UserDataDir(r.appName)
	if err != nil {
		return Paths{}, err
	}

	cacheDir, err := r.system.UserCacheDir(r.appName)
	if err != nil {
		return Paths{}, err
	}

	return Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.json"),
		HomeDir:    dataDir,
		DataDir:    dataDir,
		CacheDir:   cacheDir,
	}, nil
}
