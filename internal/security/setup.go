package security

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/mohammadirham37/jenderal_panel/internal/model"
)

var setupTimePattern = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)

type SetupRequest struct {
	ManagementCIDRs   []string `json:"management_cidrs"`
	EnableFail2ban    bool     `json:"enable_fail2ban"`
	MalwareMode       string   `json:"malware_mode"`
	ScheduleMalware   bool     `json:"schedule_malware"`
	ScheduleTime      string   `json:"schedule_time"`
	TrafficWebsiteIDs []string `json:"traffic_website_ids"`
}

type SetupReview struct {
	Mutations []string `json:"mutations"`
	Warnings  []string `json:"warnings"`
	Hash      string   `json:"hash"`
}

type SetupAssessment struct {
	Posture    PostureReport `json:"posture"`
	WebsiteIDs []string      `json:"website_ids"`
	Latest     *SetupState   `json:"latest,omitempty"`
}

type SetupState struct {
	ID             string       `json:"id"`
	Status         string       `json:"status"`
	SafeError      string       `json:"safe_error"`
	TaskID         string       `json:"task_id"`
	Request        SetupRequest `json:"request"`
	Review         SetupReview  `json:"review"`
	CompletedSteps []string     `json:"completed_steps"`
}

type SetupActions struct {
	InstallFail2ban   func(context.Context, func(string)) error
	ConfigureFail2ban func(context.Context, []string, func(string)) error
	InstallMalware    func(context.Context, string, func(string)) error
	UpdateSignatures  func(context.Context, func(string)) error
	ScheduleMalware   func(context.Context, string) error
	ObserveTraffic    func(context.Context, []string, func(string)) error
}

type SetupService struct {
	db      *sql.DB
	actions SetupActions
	posture *Service
	now     func() time.Time
}

func NewSetupService(db *sql.DB, actions SetupActions, posture *Service) *SetupService {
	return &SetupService{db: db, actions: actions, posture: posture, now: func() time.Time { return time.Now().UTC() }}
}

func (s *SetupService) Assess(ctx context.Context) (SetupAssessment, error) {
	assessment := SetupAssessment{WebsiteIDs: []string{}}
	if s.posture != nil {
		assessment.Posture = s.posture.Posture(ctx)
	} else {
		assessment.Posture = PostureReport{Components: map[string]string{}, Findings: []Finding{}}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM websites WHERE status IN ('active','suspended') ORDER BY domain`)
	if err != nil {
		return assessment, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return assessment, err
		}
		assessment.WebsiteIDs = append(assessment.WebsiteIDs, id)
	}
	if err := rows.Close(); err != nil {
		return assessment, err
	}
	latest, err := s.Latest(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return assessment, err
	}
	if err == nil {
		assessment.Latest = &latest
	}
	return assessment, nil
}

func (s *SetupService) Review(_ context.Context, request SetupRequest) (SetupReview, error) {
	for i, raw := range request.ManagementCIDRs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.IsValid() {
			return SetupReview{}, model.NewValidationError("invalid management CIDR")
		}
		request.ManagementCIDRs[i] = prefix.Masked().String()
	}
	if request.EnableFail2ban && len(request.ManagementCIDRs) == 0 {
		return SetupReview{}, model.NewValidationError("at least one management CIDR is required before enabling SSH protection")
	}
	if request.MalwareMode != "" && request.MalwareMode != "low_memory" && request.MalwareMode != "daemon" {
		return SetupReview{}, model.NewValidationError("malware mode must be low_memory or daemon")
	}
	if request.ScheduleMalware && (request.MalwareMode == "" || !setupTimePattern.MatchString(request.ScheduleTime)) {
		return SetupReview{}, model.NewValidationError("scheduled malware scan requires an HH:MM time and malware runtime")
	}

	mutations := []string{}
	if request.EnableFail2ban {
		mutations = append(mutations, "install fail2ban", "configure sshd jail")
	}
	if request.MalwareMode != "" {
		mutations = append(mutations, "install clamav ("+request.MalwareMode+")", "update clamav signatures")
	}
	if request.ScheduleMalware {
		mutations = append(mutations, "schedule daily quick scan")
	}
	if len(request.TrafficWebsiteIDs) > 0 {
		mutations = append(mutations, fmt.Sprintf("start Traffic Guard observation for %d website(s)", len(request.TrafficWebsiteIDs)))
	}
	warnings := []string{
		"Keep provider-console access available while applying SSH protection.",
		"Traffic Guard protects the HTTP layer only; volumetric DDoS protection requires a CDN or network provider.",
	}
	payload, _ := json.Marshal(struct {
		Request   SetupRequest `json:"request"`
		Mutations []string     `json:"mutations"`
		Warnings  []string     `json:"warnings"`
	}{request, mutations, warnings})
	sum := sha256.Sum256(payload)
	return SetupReview{Mutations: mutations, Warnings: warnings, Hash: hex.EncodeToString(sum[:])}, nil
}

func (s *SetupService) CreateRun(ctx context.Context, request SetupRequest, review SetupReview) (SetupState, error) {
	expected, err := s.Review(ctx, request)
	if err != nil {
		return SetupState{}, err
	}
	if review.Hash == "" || review.Hash != expected.Hash {
		return SetupState{}, model.NewValidationError("setup review is stale; review the requested changes again")
	}
	id, now := ulid.Make().String(), s.now()
	requestJSON, _ := json.Marshal(request)
	reviewJSON, _ := json.Marshal(expected)
	_, err = s.db.ExecContext(ctx, `INSERT INTO security_setup_runs(id,request_json,review_json,review_hash,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id, string(requestJSON), string(reviewJSON), expected.Hash, "pending", setupTS(now), setupTS(now))
	if err != nil {
		return SetupState{}, err
	}
	return SetupState{ID: id, Status: "pending", Request: request, Review: expected, CompletedSteps: []string{}}, nil
}

func (s *SetupService) BindTask(ctx context.Context, runID, taskID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE security_setup_runs SET task_id=?,updated_at=? WHERE id=?`, taskID, setupTS(s.now()), runID)
	return err
}

func (s *SetupService) Execute(ctx context.Context, runID string, log func(string)) error {
	state, err := s.State(ctx, runID)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE security_setup_runs SET status='running',safe_error='',updated_at=? WHERE id=?`, setupTS(s.now()), runID)
	done := map[string]bool{}
	for _, step := range state.CompletedSteps {
		done[step] = true
	}

	type setupStep struct {
		name  string
		label string
		run   func() error
	}
	steps := []setupStep{}
	if state.Request.EnableFail2ban {
		steps = append(steps,
			setupStep{"fail2ban_install", "Install Fail2ban", func() error { return requireSetupAction(s.actions.InstallFail2ban, ctx, log) }},
			setupStep{"fail2ban_configure", "Configure Fail2ban", func() error {
				if s.actions.ConfigureFail2ban == nil {
					return errors.New("Fail2ban configuration action is unavailable")
				}
				return s.actions.ConfigureFail2ban(ctx, state.Request.ManagementCIDRs, log)
			}},
		)
	}
	if state.Request.MalwareMode != "" {
		steps = append(steps,
			setupStep{"malware_install", "Install ClamAV", func() error {
				if s.actions.InstallMalware == nil {
					return errors.New("malware installation action is unavailable")
				}
				return s.actions.InstallMalware(ctx, state.Request.MalwareMode, log)
			}},
			setupStep{"malware_signatures", "Update ClamAV signatures", func() error { return requireSetupAction(s.actions.UpdateSignatures, ctx, log) }},
		)
	}
	if state.Request.ScheduleMalware {
		steps = append(steps, setupStep{"malware_schedule", "Save malware schedule", func() error {
			if s.actions.ScheduleMalware == nil {
				return errors.New("malware schedule action is unavailable")
			}
			return s.actions.ScheduleMalware(ctx, state.Request.ScheduleTime)
		}})
	}
	if len(state.Request.TrafficWebsiteIDs) > 0 {
		steps = append(steps, setupStep{"traffic_observe", "Start Traffic Guard observation", func() error {
			if s.actions.ObserveTraffic == nil {
				return errors.New("Traffic Guard action is unavailable")
			}
			return s.actions.ObserveTraffic(ctx, state.Request.TrafficWebsiteIDs, log)
		}})
	}

	for _, current := range steps {
		if done[current.name] {
			continue
		}
		if log != nil {
			log("=== " + current.label + " ===\n")
		}
		if err := current.run(); err != nil {
			_ = s.markStep(ctx, runID, current.name, err)
			_, _ = s.db.ExecContext(context.Background(), `UPDATE security_setup_runs SET status='failed',safe_error=?,updated_at=? WHERE id=?`, safeSetupError(err), setupTS(s.now()), runID)
			return err
		}
		if err := s.markStep(ctx, runID, current.name, nil); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE security_setup_runs SET status='completed',safe_error='',updated_at=? WHERE id=?`, setupTS(s.now()), runID)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO security_settings(key,value,updated_at) VALUES('security.setup_complete','1',?) ON CONFLICT(key) DO UPDATE SET value='1',updated_at=excluded.updated_at`, setupTS(s.now()))
	}
	return err
}

func requireSetupAction(action func(context.Context, func(string)) error, ctx context.Context, log func(string)) error {
	if action == nil {
		return errors.New("setup action is unavailable")
	}
	return action(ctx, log)
}
func safeSetupError(err error) string {
	value := strings.TrimSpace(err.Error())
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}
func setupTS(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func (s *SetupService) markStep(ctx context.Context, runID, name string, cause error) error {
	status, safeError := "completed", ""
	var completed any = setupTS(s.now())
	if cause != nil {
		status, safeError, completed = "failed", safeSetupError(cause), nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO security_setup_steps(run_id,step_name,status,safe_error,completed_at,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(run_id,step_name) DO UPDATE SET status=excluded.status,safe_error=excluded.safe_error,completed_at=excluded.completed_at,updated_at=excluded.updated_at`, runID, name, status, safeError, completed, setupTS(s.now()))
	return err
}

func (s *SetupService) Latest(ctx context.Context) (SetupState, error) {
	var id string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM security_setup_runs ORDER BY updated_at DESC LIMIT 1`).Scan(&id); err != nil {
		return SetupState{}, err
	}
	return s.State(ctx, id)
}

func (s *SetupService) State(ctx context.Context, id string) (SetupState, error) {
	var state SetupState
	var requestJSON, reviewJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id,status,safe_error,task_id,request_json,review_json FROM security_setup_runs WHERE id=?`, id).Scan(&state.ID, &state.Status, &state.SafeError, &state.TaskID, &requestJSON, &reviewJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return state, model.ErrNotFound
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal([]byte(requestJSON), &state.Request); err != nil {
		return state, err
	}
	if err = json.Unmarshal([]byte(reviewJSON), &state.Review); err != nil {
		return state, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT step_name FROM security_setup_steps WHERE run_id=? AND status='completed' ORDER BY completed_at`, id)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return state, err
		}
		state.CompletedSteps = append(state.CompletedSteps, name)
	}
	sort.Strings(state.CompletedSteps)
	return state, rows.Err()
}
