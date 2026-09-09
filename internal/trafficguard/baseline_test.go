package trafficguard

import (
	"testing"
	"time"
)

func TestBaselineCannotAlertBeforeSufficientSamples(t *testing.T) {
	now := time.Now()
	first := now.Add(-25 * time.Hour)
	b := Baseline{SampleCount: 10, MeanRPM: 10, M2RPM: 9, FirstSampleAt: &first}
	if got := Evaluate(b, MinuteBucket{BucketAt: now, Requests: 1000}); len(got) != 0 {
		t.Fatalf("premature anomalies=%v", got)
	}
}
func TestBaselineAlertsAfterSufficientHistory(t *testing.T) {
	now := time.Now()
	first := now.Add(-25 * time.Hour)
	b := Baseline{SampleCount: 1440, MeanRPM: 10, M2RPM: 100, FirstSampleAt: &first}
	if got := Evaluate(b, MinuteBucket{BucketAt: now, Requests: 1000}); len(got) != 1 {
		t.Fatalf("anomalies=%v", got)
	}
}
