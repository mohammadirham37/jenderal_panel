package backup

import (
	"testing"
	"time"
)

func TestRetentionExpired(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-30 * 24 * time.Hour)
	fresh := now.Add(-1 * time.Hour)

	cases := []struct {
		name             string
		days, keep, rank int
		createdAt        time.Time
		want             bool
	}{
		{"within retention", 7, 0, 1, fresh, false},
		{"older than days", 7, 0, 1, old, true},
		{"pushed out of keep-N", 0, 3, 4, fresh, true},
		{"inside keep-N", 0, 3, 3, fresh, false},
		{"keep-N ignores age when days off", 0, 5, 2, old, false},
		{"days ignores rank when keep off", 7, 0, 9, fresh, false},
		{"no retention configured", 0, 0, 1, old, false},
	}
	for _, c := range cases {
		if got := retentionExpired(c.days, c.keep, c.rank, c.createdAt, now); got != c.want {
			t.Errorf("%s: retentionExpired = %v, want %v", c.name, got, c.want)
		}
	}
}
