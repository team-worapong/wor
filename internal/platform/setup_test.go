package platform

import "testing"

func TestDefaultSetupEnvironment(t *testing.T) {
	t.Parallel()

	if got := New("linux", "amd64").DefaultSetupEnvironment(); got != "production" {
		t.Fatalf("linux default environment = %q", got)
	}
	if got := New("darwin", "arm64").DefaultSetupEnvironment(); got != "development" {
		t.Fatalf("darwin default environment = %q", got)
	}
	if got := New("windows", "amd64").DefaultSetupEnvironment(); got != "development" {
		t.Fatalf("windows default environment = %q", got)
	}
}

func TestDefaultWORHomeForProduction(t *testing.T) {
	t.Parallel()

	got, err := New("linux", "amd64").DefaultWORHome("production")
	if err != nil {
		t.Fatalf("default WOR_HOME: %v", err)
	}
	if got != "/opt/wor" {
		t.Fatalf("linux production WOR_HOME = %q", got)
	}

	got, err = New("windows", "amd64").DefaultWORHome("production")
	if err != nil {
		t.Fatalf("default WOR_HOME: %v", err)
	}
	if got != `C:\WOR` {
		t.Fatalf("windows production WOR_HOME = %q", got)
	}
}
