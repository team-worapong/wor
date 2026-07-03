package version

const UnknownValue = "unknown"

var (
	// These values can be overridden by -ldflags during release builds.
	Version   = "0.1.0-dev"
	Commit    = UnknownValue
	BuildDate = UnknownValue
)

type Info struct {
	Name      string
	Version   string
	Commit    string
	BuildDate string
}

func Current() Info {
	return Info{
		Name:      "WOR",
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	}
}

func (i Info) String() string {
	return i.Name + " " + i.Version
}
