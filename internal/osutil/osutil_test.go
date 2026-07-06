package osutil

import "testing"

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		wantName string
		wantOK   bool
	}{
		{
			name: "debian-style PRETTY_NAME",
			content: `PRETTY_NAME="Debian GNU/Linux 13 (trixie)"
NAME="Debian GNU/Linux"
ID=debian
`,
			wantName: "Debian GNU/Linux 13 (trixie)",
			wantOK:   true,
		},
		{
			name: "ubuntu-style PRETTY_NAME",
			content: `PRETTY_NAME="Ubuntu 22.04.5 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
ID=ubuntu
ID_LIKE=debian
`,
			wantName: "Ubuntu 22.04.5 LTS",
			wantOK:   true,
		},
		{
			name: "no PRETTY_NAME falls back to ID",
			content: `NAME="Some Distro"
ID=somedistro
`,
			wantName: "somedistro",
			wantOK:   true,
		},
		{
			name:     "neither field present",
			content:  `NAME="Mystery"` + "\n",
			wantName: "",
			wantOK:   false,
		},
		{
			name:     "empty file",
			content:  "",
			wantName: "",
			wantOK:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotName, gotOK := parseOSRelease([]byte(c.content))
			if gotName != c.wantName || gotOK != c.wantOK {
				t.Errorf("parseOSRelease(%q) = (%q, %v), want (%q, %v)", c.content, gotName, gotOK, c.wantName, c.wantOK)
			}
		})
	}
}

func TestParseOSReleaseIDFields(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantID     string
		wantIDLike []string
	}{
		{
			name:       "debian",
			content:    "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n",
			wantID:     "debian",
			wantIDLike: nil,
		},
		{
			name:       "ubuntu",
			content:    "PRETTY_NAME=\"Ubuntu 22.04.5 LTS\"\nID=ubuntu\nID_LIKE=debian\n",
			wantID:     "ubuntu",
			wantIDLike: []string{"debian"},
		},
		{
			name:       "rocky linux (multi-value ID_LIKE)",
			content:    "PRETTY_NAME=\"Rocky Linux 9\"\nID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n",
			wantID:     "rocky",
			wantIDLike: []string{"rhel", "centos", "fedora"},
		},
		{
			name:       "empty file",
			content:    "",
			wantID:     "",
			wantIDLike: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotID, gotIDLike := parseOSReleaseIDFields([]byte(c.content))
			if gotID != c.wantID || !stringSlicesEqual(gotIDLike, c.wantIDLike) {
				t.Errorf("parseOSReleaseIDFields(%q) = (%q, %v), want (%q, %v)", c.content, gotID, gotIDLike, c.wantID, c.wantIDLike)
			}
		})
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestIsDebianFamilyFields(t *testing.T) {
	cases := []struct {
		name string
		id   string
		like []string
		want bool
	}{
		{"debian itself", "debian", nil, true},
		{"ubuntu via ID_LIKE", "ubuntu", []string{"debian"}, true},
		{"rocky is not debian family", "rocky", []string{"rhel", "centos", "fedora"}, false},
		{"empty fields", "", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDebianFamilyFields(c.id, c.like); got != c.want {
				t.Errorf("isDebianFamilyFields(%q, %v) = %v, want %v", c.id, c.like, got, c.want)
			}
		})
	}
}
