package engine

import (
	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/domain"
	"github.com/team-worapong/wor/internal/service"
	"github.com/team-worapong/wor/internal/version"
)

type VersionReport struct {
	Name               string
	Version            string
	Commit             string
	BuildDate          string
	CommitAvailable    bool
	BuildDateAvailable bool
}

func newVersionReport(info version.Info) VersionReport {
	return VersionReport{
		Name:               info.Name,
		Version:            info.Version,
		Commit:             info.Commit,
		BuildDate:          info.BuildDate,
		CommitAvailable:    info.Commit != "" && info.Commit != version.UnknownValue,
		BuildDateAvailable: info.BuildDate != "" && info.BuildDate != version.UnknownValue,
	}
}

func (r VersionReport) String() string {
	return r.Name + " " + r.Version
}

type HelpReport struct {
	Title    string
	Usage    string
	Commands []CommandHelp
}

type CommandHelp struct {
	Name        string
	Description string
}

type EnvironmentReport struct {
	Runtime     RuntimeReport
	Config      config.Config
	Environment []EnvironmentVariable
}

type RuntimeReport struct {
	Version   string
	OS        string
	Arch      string
	Supported bool
}

type EnvironmentVariable struct {
	Name  string
	Value string
	Set   bool
}

type DomainReport struct {
	Domain   domain.Metadata
	Services []service.Metadata
}

type ServiceReport struct {
	Service             service.Metadata
	Template            service.Template
	RuntimeRequirements []string
	ProcessRequirements []string
}
