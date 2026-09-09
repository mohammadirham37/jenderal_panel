package security

import "time"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type EventStatus string

const (
	StatusOpen          EventStatus = "open"
	StatusAcknowledged  EventStatus = "acknowledged"
	StatusResolved      EventStatus = "resolved"
	StatusFalsePositive EventStatus = "false_positive"
)

type EventInput struct {
	Fingerprint       string   `json:"fingerprint"`
	Category          string   `json:"category"`
	Severity          Severity `json:"severity"`
	Component         string   `json:"component"`
	Resource          string   `json:"resource"`
	Evidence          string   `json:"evidence"`
	RecommendedAction string   `json:"recommended_action"`
}

type Event struct {
	ID string `json:"id"`
	EventInput
	Status          EventStatus `json:"status"`
	OccurrenceCount int         `json:"occurrence_count"`
	FirstSeen       time.Time   `json:"first_seen"`
	LastSeen        time.Time   `json:"last_seen"`
	NotifiedAt      *time.Time  `json:"notified_at,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type EventFilter struct {
	Status    EventStatus
	Severity  Severity
	Component string
	Limit     int
	Offset    int
}

func validSeverity(value Severity) bool {
	switch value {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

func validStatus(value EventStatus) bool {
	switch value {
	case StatusOpen, StatusAcknowledged, StatusResolved, StatusFalsePositive:
		return true
	default:
		return false
	}
}
