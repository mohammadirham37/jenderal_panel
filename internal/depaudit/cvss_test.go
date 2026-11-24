package depaudit

import "testing"

func TestCVSS3BaseScore(t *testing.T) {
	cases := map[string]float64{
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H": 9.8,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N": 6.1,
		"CVSS:3.0/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N": 5.5,
		"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:H/I:H/A:H": 9.9,
		"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:N/I:N/A:N": 0,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H": 7.5,
	}
	for vector, want := range cases {
		got, ok := CVSS3BaseScore(vector)
		if !ok || got != want {
			t.Errorf("%s = %v, %v; want %v", vector, got, ok, want)
		}
	}
	for _, bad := range []string{"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N", "AV:N/AC:L", "CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", ""} {
		if _, ok := CVSS3BaseScore(bad); ok {
			t.Errorf("%q must not be scored", bad)
		}
	}
}

func TestSeverityFromScore(t *testing.T) {
	for score, want := range map[float64]string{9.8: "critical", 9.0: "critical", 7.5: "high", 4.0: "medium", 3.9: "low", 0: "unknown"} {
		if got := severityFromScore(score); got != want {
			t.Errorf("severityFromScore(%v) = %q, want %q", score, got, want)
		}
	}
}
