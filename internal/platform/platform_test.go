package platform

import "testing"

func TestSupportedPlatforms(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"linux", "darwin", "windows"} {
		if !isSupportedOS(goos) {
			t.Fatalf("expected %s to be supported", goos)
		}
	}

	if isSupportedOS("plan9") {
		t.Fatal("plan9 should not be supported")
	}
}

func TestSupportedArchitectures(t *testing.T) {
	t.Parallel()

	for _, goarch := range []string{"amd64", "arm64"} {
		if !isSupportedArch(goarch) {
			t.Fatalf("expected %s to be supported", goarch)
		}
	}

	if isSupportedArch("386") {
		t.Fatal("386 should not be supported")
	}
}
