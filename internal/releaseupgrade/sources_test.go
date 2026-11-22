package releaseupgrade

import (
	"reflect"
	"testing"
)

func TestMapSuite(t *testing.T) {
	cases := map[string]string{
		"noble":             "resolute",
		"noble-updates":     "resolute-updates",
		"noble-security":    "resolute-security",
		"noble-backports":   "resolute-backports",
		"noble-pgdg":        "resolute-pgdg",
		"resolute":          "resolute",
		"resolute-security": "resolute-security",
	}
	for in, want := range cases {
		got, ok := MapSuite(in)
		if !ok || got != want {
			t.Errorf("MapSuite(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"jammy", "stable", "noble-foo", "bookworm"} {
		if _, ok := MapSuite(in); ok {
			t.Errorf("MapSuite(%q) must not map", in)
		}
	}
}

func TestParseListFile(t *testing.T) {
	content := `# comment
deb [signed-by=/usr/share/keyrings/sury-php.gpg] https://packages.sury.org/php/ noble main
deb [ arch=amd64 signed-by=/x.gpg ] https://example.com/repo stable main
# deb http://disabled.example.com noble main
deb-src http://archive.ubuntu.com/ubuntu noble main
deb http://mirrors.tencentyun.com/ubuntu noble-updates main restricted # trailing
broken line
`
	got := ParseListFile("/etc/apt/sources.list.d/x.list", content)
	want := []AptSource{
		{File: "/etc/apt/sources.list.d/x.list", URI: "https://packages.sury.org/php/", Suites: []string{"noble"}, Types: []string{"deb"}},
		{File: "/etc/apt/sources.list.d/x.list", URI: "https://example.com/repo", Suites: []string{"stable"}, Types: []string{"deb"}},
		{File: "/etc/apt/sources.list.d/x.list", URI: "http://archive.ubuntu.com/ubuntu", Suites: []string{"noble"}, Types: []string{"deb-src"}},
		{File: "/etc/apt/sources.list.d/x.list", URI: "http://mirrors.tencentyun.com/ubuntu", Suites: []string{"noble-updates"}, Types: []string{"deb"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseDeb822File(t *testing.T) {
	content := `# Ubuntu sources
Types: deb
URIs: http://mirrors.tencentyun.com/ubuntu
Suites: noble noble-updates noble-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg

Types: deb
URIs: http://security.ubuntu.com/ubuntu
Suites: noble-security
Components: main

Types: deb
URIs: https://disabled.example.com
Suites: noble
Enabled: no
`
	got := ParseDeb822File("/etc/apt/sources.list.d/ubuntu.sources", content)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].URI != "http://mirrors.tencentyun.com/ubuntu" || len(got[0].Suites) != 3 {
		t.Errorf("first stanza = %+v", got[0])
	}
	if got[1].Suites[0] != "noble-security" {
		t.Errorf("second stanza = %+v", got[1])
	}
}

func TestSourceClassification(t *testing.T) {
	sury := AptSource{URI: "https://packages.sury.org/php/"}
	pgdg := AptSource{URI: "https://apt.postgresql.org/pub/repos/apt"}
	archive := AptSource{URI: "http://id.archive.ubuntu.com/ubuntu"}
	tencent := AptSource{URI: "http://mirrors.tencentyun.com/ubuntu"}
	if !sury.PanelManaged() || !pgdg.PanelManaged() || tencent.PanelManaged() {
		t.Error("panel-managed classification is wrong")
	}
	if !archive.UbuntuArchive() || tencent.UbuntuArchive() {
		t.Error("ubuntu archive classification is wrong")
	}
}
