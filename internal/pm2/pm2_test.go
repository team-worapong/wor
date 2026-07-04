package pm2

import (
	"strconv"
	"testing"
	"time"
)

func TestParseJlist(t *testing.T) {
	now := time.Now()
	startedMs := now.Add(-90 * time.Minute).UnixMilli()

	data := []byte(`[
		{
			"name": "wor_shop_webapp",
			"pid": 41213,
			"pm2_env": {"status": "online", "pm_uptime": ` + strconv.FormatInt(startedMs, 10) + `},
			"monit": {"cpu": 0.4, "memory": 138412032}
		},
		{
			"name": "wor_shop_notifier",
			"pid": 0,
			"pm2_env": {"status": "stopped", "pm_uptime": 0},
			"monit": {"cpu": 0, "memory": 0}
		}
	]`)

	procs, err := parseJlist(data)
	if err != nil {
		t.Fatalf("parseJlist: %v", err)
	}
	if len(procs) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(procs))
	}

	webapp, ok := procs["wor_shop_webapp"]
	if !ok {
		t.Fatal("missing wor_shop_webapp entry")
	}
	if webapp.PID != 41213 {
		t.Errorf("PID = %d, want 41213", webapp.PID)
	}
	if webapp.Status != "online" {
		t.Errorf("Status = %q, want online", webapp.Status)
	}
	if webapp.Uptime < 89*time.Minute || webapp.Uptime > 91*time.Minute {
		t.Errorf("Uptime = %v, want ~90m", webapp.Uptime)
	}
	if webapp.CPUPercent != 0.4 {
		t.Errorf("CPUPercent = %v, want 0.4", webapp.CPUPercent)
	}
	if webapp.MemoryBytes != 138412032 {
		t.Errorf("MemoryBytes = %d, want 138412032", webapp.MemoryBytes)
	}

	notifier, ok := procs["wor_shop_notifier"]
	if !ok {
		t.Fatal("missing wor_shop_notifier entry")
	}
	if notifier.Status != "stopped" {
		t.Errorf("Status = %q, want stopped", notifier.Status)
	}
	if notifier.Uptime != 0 {
		t.Errorf("Uptime = %v, want 0 (pm_uptime was 0)", notifier.Uptime)
	}
}

func TestParseJlistInvalidJSON(t *testing.T) {
	if _, err := parseJlist([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON, got nil")
	}
}

func TestParseJlistEmpty(t *testing.T) {
	procs, err := parseJlist([]byte("[]"))
	if err != nil {
		t.Fatalf("parseJlist: %v", err)
	}
	if len(procs) != 0 {
		t.Errorf("expected 0 processes, got %d", len(procs))
	}
}
