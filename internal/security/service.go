package security

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mohammadirham37/jenderal_panel/internal/taskrunner"
)

const (
	ConditionGood           = "good"
	ConditionNeedsAttention = "needs_attention"
	ConditionCritical       = "critical"
)

type ComponentStatus struct {
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Version   string    `json:"version,omitempty"`
	Message   string    `json:"message,omitempty"`
	Installed bool      `json:"installed"`
	Enabled   bool      `json:"enabled"`
	Healthy   bool      `json:"healthy"`
	CheckedAt time.Time `json:"checked_at"`
}

type Probe interface {
	Name() string
	Check(context.Context) ComponentStatus
}

type Overview struct {
	Condition     string            `json:"condition"`
	Reasons       []string          `json:"reasons"`
	Components    []ComponentStatus `json:"components"`
	OpenEvents    int               `json:"open_events"`
	SetupComplete bool              `json:"setup_complete"`
	ActiveTasks   []taskrunner.Task `json:"active_tasks"`
	Posture       *PostureReport    `json:"posture,omitempty"`
}

type Service struct {
	events      *EventService
	tasks       *taskrunner.Runner
	probes      []Probe
	posture     *PostureChecker
	postureMu   sync.RWMutex
	lastPosture PostureReport
}

func (s *Service) SetPostureChecker(checker *PostureChecker) { s.posture = checker }

func (s *Service) RefreshPosture(ctx context.Context, now time.Time) PostureReport {
	if s.posture == nil {
		return PostureReport{CheckedAt: now.UTC(), Components: map[string]string{}, Findings: []Finding{}}
	}
	report := s.posture.Check(ctx, now)
	s.postureMu.Lock()
	s.lastPosture = report
	s.postureMu.Unlock()
	return report
}

func (s *Service) Posture(ctx context.Context) PostureReport {
	s.postureMu.RLock()
	report := s.lastPosture
	s.postureMu.RUnlock()
	if report.CheckedAt.IsZero() {
		return s.RefreshPosture(ctx, time.Now().UTC())
	}
	return report
}

func NewService(events *EventService, tasks *taskrunner.Runner, probes ...Probe) *Service {
	return &Service{events: events, tasks: tasks, probes: probes}
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	components := make([]ComponentStatus, 0, len(s.probes))
	for _, probe := range s.probes {
		status := probe.Check(ctx)
		if status.Name == "" {
			status.Name = probe.Name()
		}
		if !status.Installed && status.State == "" {
			status.State = "not_installed"
		}
		if status.CheckedAt.IsZero() {
			status.CheckedAt = time.Now().UTC()
		}
		components = append(components, status)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })

	_, openEvents, err := s.activeEventCounts(ctx)
	if err != nil {
		return Overview{}, err
	}
	report := s.Posture(ctx)
	activeEvents, _, err := s.events.List(ctx, EventFilter{Limit: 200})
	if err != nil {
		return Overview{}, err
	}
	score := Score(report, activeEvents)
	condition := score.Level
	reasons := append([]string{}, score.Reasons...)
	if len(reasons) == 1 && reasons[0] == "No active security issues detected." {
		reasons = nil
	}
	for _, component := range components {
		if component.Enabled && !component.Healthy {
			if condition == ConditionGood {
				condition = ConditionNeedsAttention
			}
			reason := component.Message
			if reason == "" {
				reason = "enabled but unhealthy"
			}
			reasons = append(reasons, fmt.Sprintf("%s: %s.", component.Name, strings.TrimSuffix(reason, ".")))
		}
	}
	if score.Additional > 0 {
		reasons = append(reasons, fmt.Sprintf("%d additional finding(s) are available in posture details.", score.Additional))
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "No active security issues detected.")
	}

	setupComplete, err := s.setupComplete(ctx)
	if err != nil {
		return Overview{}, err
	}
	activeTasks := make([]taskrunner.Task, 0)
	if s.tasks != nil {
		for _, task := range s.tasks.List() {
			if task.Module == "security" && task.Status == "running" {
				activeTasks = append(activeTasks, task)
			}
		}
	}
	return Overview{
		Condition: condition, Reasons: reasons, Components: components,
		OpenEvents: openEvents, SetupComplete: setupComplete, ActiveTasks: activeTasks, Posture: &report,
	}, nil
}

func (s *Service) ListEvents(ctx context.Context, filter EventFilter) ([]Event, int, error) {
	return s.events.List(ctx, filter)
}

func (s *Service) TransitionEvent(ctx context.Context, id string, status EventStatus, now time.Time) error {
	return s.events.Transition(ctx, id, status, now)
}

func (s *Service) activeEventCounts(ctx context.Context) (map[Severity]int, int, error) {
	rows, err := s.events.db.QueryContext(ctx, `SELECT severity, COUNT(*) FROM security_events
		WHERE status IN ('open','acknowledged') GROUP BY severity`)
	if err != nil {
		return nil, 0, fmt.Errorf("count active security events: %w", err)
	}
	defer rows.Close()
	counts := make(map[Severity]int)
	total := 0
	for rows.Next() {
		var severity Severity
		var count int
		if err := rows.Scan(&severity, &count); err != nil {
			return nil, 0, fmt.Errorf("scan active security event count: %w", err)
		}
		counts[severity] = count
		total += count
	}
	return counts, total, rows.Err()
}

func (s *Service) setupComplete(ctx context.Context) (bool, error) {
	var value string
	err := s.events.db.QueryRowContext(ctx, `SELECT value FROM security_settings WHERE key = 'security.setup_complete'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load security setup state: %w", err)
	}
	return value == "1" || strings.EqualFold(value, "true"), nil
}
