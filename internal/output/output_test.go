package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableRendersHeadersAndRows(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := New(&stdout, &bytes.Buffer{}, FormatText)

	renderer.Table(
		[]string{"Name", "Status"},
		[][]string{{"Go", "ok"}, {"PM2", "missing"}},
	)

	got := stdout.String()
	for _, want := range []string{"Name", "Status", "Go", "ok", "PM2", "missing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table output missing %q:\n%s", want, got)
		}
	}
}

func TestWarningUsesStderr(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := New(&stdout, &stderr, FormatText)

	renderer.Warning("careful")

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[WARN] careful") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
