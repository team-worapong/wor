package cliapp

import "testing"

func TestCommandNeedsLock(t *testing.T) {
	cases := []struct {
		cmd  string
		rest []string
		want bool
	}{
		{"version", nil, false},
		{"--version", nil, false},
		{"-v", nil, false},
		{"help", nil, false},
		{"-h", nil, false},
		{"--help", nil, false},
		{"", nil, false},
		{"service", []string{"logs", "shop/web"}, false},
		{"host", []string{"logs", "shop.test"}, false},
		{"service", []string{"status"}, true},
		{"service", nil, true},
		{"host", []string{"list"}, true},
		{"create", nil, true},
		{"setup", nil, true},
		{"deploy", []string{"shop.test"}, true},
		{"run", nil, true},
		{"doctor", nil, true},
	}
	for _, c := range cases {
		got := commandNeedsLock(c.cmd, c.rest)
		if got != c.want {
			t.Errorf("commandNeedsLock(%q, %v) = %v, want %v", c.cmd, c.rest, got, c.want)
		}
	}
}
