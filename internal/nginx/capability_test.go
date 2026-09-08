package nginx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIPv6AvailableAt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		missing bool
		want    bool
	}{
		{name: "interface present", content: "00000000000000000000000000000001 01 80 10 80 lo\n", want: true},
		{name: "disabled", content: "\n", want: false},
		{name: "probe unavailable", missing: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "if_inet6")
			if !tt.missing {
				if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
			}

			if got := ipv6AvailableAt(path); got != tt.want {
				t.Fatalf("ipv6AvailableAt() = %v, want %v", got, tt.want)
			}
		})
	}
}
