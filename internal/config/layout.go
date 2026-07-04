package config

import "path/filepath"

type WORHomeLayout struct {
	Home      string
	Domains   string
	Runtime   string
	Templates string
	Logs      string
	SSL       string
	Configs   string
	Cache     string
	Data      string
	Backups   string
}

func Layout(cfg Config) WORHomeLayout {
	return LayoutForHome(cfg.WORHome)
}

func LayoutForHome(worHome string) WORHomeLayout {
	return WORHomeLayout{
		Home:      worHome,
		Domains:   filepath.Join(worHome, "domains"),
		Runtime:   filepath.Join(worHome, "runtime"),
		Templates: filepath.Join(worHome, "templates"),
		Logs:      filepath.Join(worHome, "logs"),
		SSL:       filepath.Join(worHome, "ssl"),
		Configs:   filepath.Join(worHome, "configs"),
		Cache:     filepath.Join(worHome, "cache"),
		Data:      filepath.Join(worHome, "data"),
		Backups:   filepath.Join(worHome, "backups"),
	}
}

func (l WORHomeLayout) Directories() []string {
	return []string{
		l.Domains,
		l.Runtime,
		l.Templates,
		l.Logs,
		l.SSL,
		l.Configs,
		l.Cache,
		l.Data,
		l.Backups,
	}
}
