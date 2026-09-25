package website

import "testing"

func TestValidateUpstreamAcceptsValidTargets(t *testing.T) {
	cases := []struct {
		scheme, host string
		port         int
		want         string
	}{
		{"http", "127.0.0.1", 3000, "http://127.0.0.1:3000"},
		{"https", "10.0.0.5", 8080, "https://10.0.0.5:8080"},
		{"http", "app.internal.lan", 80, "http://app.internal.lan:80"},
		{"HTTP", "app.example.com", 9000, "http://app.example.com:9000"},
		{"http", "  web-1.local  ", 80, "http://web-1.local:80"},
		{"https", "::1", 3000, "https://[::1]:3000"},
		{"http", "localhost", 5601, "http://localhost:5601"},
	}
	for _, tc := range cases {
		up, err := ValidateUpstream(tc.scheme, tc.host, tc.port)
		if err != nil {
			t.Fatalf("ValidateUpstream(%q,%q,%d): unexpected error %v", tc.scheme, tc.host, tc.port, err)
		}
		if up.Target() != tc.want {
			t.Fatalf("ValidateUpstream(%q,%q,%d) = %q, want %q", tc.scheme, tc.host, tc.port, up.Target(), tc.want)
		}
	}
}

func TestValidateUpstreamRejectsInvalidTargets(t *testing.T) {
	cases := []struct {
		scheme, host string
		port         int
	}{
		{"ftp", "127.0.0.1", 3000},
		{"", "127.0.0.1", 3000},
		{"http", "", 3000},
		{"http", "not a host", 3000},
		{"http", "bad/host", 3000},
		{"http", "bad:3000", 3000},
		{"http", "bad;host", 3000},
		{"http", "bad%20host", 3000},
		{"http", "127.0.0.1", 0},
		{"http", "127.0.0.1", -1},
		{"http", "127.0.0.1", 70000},
	}
	for _, tc := range cases {
		if _, err := ValidateUpstream(tc.scheme, tc.host, tc.port); err == nil {
			t.Fatalf("ValidateUpstream(%q,%q,%d): expected error", tc.scheme, tc.host, tc.port)
		}
	}
}
