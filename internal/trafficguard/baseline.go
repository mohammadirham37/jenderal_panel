package trafficguard

import (
	"math"
	"time"
)

func UpdateBaseline(b Baseline, m MinuteBucket) Baseline {
	x := float64(m.Requests)
	b.SampleCount++
	if b.FirstSampleAt == nil {
		v := m.BucketAt
		b.FirstSampleAt = &v
	}
	delta := x - b.MeanRPM
	b.MeanRPM += delta / float64(b.SampleCount)
	b.M2RPM += delta * (x - b.MeanRPM)
	b.UpdatedAt = m.BucketAt.UTC()
	return b
}
func Evaluate(b Baseline, m MinuteBucket) []Anomaly {
	if b.SampleCount < 1440 || b.FirstSampleAt == nil || m.BucketAt.Sub(*b.FirstSampleAt) < 24*time.Hour {
		return nil
	}
	sd := 0.0
	if b.SampleCount > 1 {
		sd = math.Sqrt(b.M2RPM / float64(b.SampleCount-1))
	}
	threshold := math.Max(600, b.MeanRPM+4*sd)
	if float64(m.Requests) <= threshold {
		return nil
	}
	severity := "warning"
	if float64(m.Requests) > threshold*2 && m.Status5xx*10 > m.Requests {
		severity = "critical"
	}
	return []Anomaly{{Signal: "request_rate", Severity: severity, Message: "HTTP request volume exceeded the established website baseline.", Value: float64(m.Requests), Threshold: threshold}}
}
