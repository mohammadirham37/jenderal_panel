package auth

import "testing"

func TestCanManageResource(t *testing.T) {
	cases := []struct {
		name      string
		isAdmin   bool
		userID    string
		createdBy string
		want      bool
	}{
		{"admin acts on anything", true, "u1", "", true},
		{"admin acts on foreign resource", true, "u1", "u2", true},
		{"owner acts on own resource", false, "u2", "u2", true},
		{"user blocked from foreign resource", false, "u1", "u2", false},
		{"user blocked from legacy resource", false, "u1", "", false},
	}
	for _, c := range cases {
		if got := CanManageResource(c.isAdmin, c.userID, c.createdBy); got != c.want {
			t.Errorf("%s: CanManageResource = %v, want %v", c.name, got, c.want)
		}
	}
}
