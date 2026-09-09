package security

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mohammadirham37/jenderal_panel/internal/database"
)

type recordingNotifier struct {
	messages []string
}

func (n *recordingNotifier) SendAll(_ context.Context, message string) error {
	n.messages = append(n.messages, message)
	return nil
}

func newEventTestService(t *testing.T, notifier NotificationSender) (*EventService, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := database.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewEventService(db, notifier), db
}

func validEventInput() EventInput {
	return EventInput{
		Fingerprint:       "fail2ban:sshd:203.0.113.7",
		Category:          "intrusion",
		Severity:          SeverityHigh,
		Component:         "fail2ban",
		Resource:          "sshd",
		Evidence:          `{"ip":"203.0.113.7"}`,
		RecommendedAction: "Review SSH authentication logs.",
	}
}

func TestRecordDeduplicatesOpenEventAndStoresOccurrence(t *testing.T) {
	notifier := &recordingNotifier{}
	svc, db := newEventTestService(t, notifier)
	input := validEventInput()
	firstAt := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	first, created, err := svc.Record(context.Background(), input, firstAt)
	if err != nil || !created {
		t.Fatalf("first record: created=%v err=%v", created, err)
	}
	second, created, err := svc.Record(context.Background(), input, firstAt.Add(time.Minute))
	if err != nil || created || second.ID != first.ID || second.OccurrenceCount != 2 {
		t.Fatalf("second = %#v created=%v err=%v", second, created, err)
	}

	var occurrences int
	if err := db.QueryRow(`SELECT COUNT(*) FROM security_event_occurrences WHERE event_id = ?`, first.ID).Scan(&occurrences); err != nil {
		t.Fatal(err)
	}
	if occurrences != 2 {
		t.Fatalf("occurrences = %d, want 2", occurrences)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("notifications = %d, want one for the new high severity event", len(notifier.messages))
	}
}

func TestRecordRejectsInvalidEnumsAndRequiredFields(t *testing.T) {
	svc, _ := newEventTestService(t, nil)
	input := validEventInput()
	input.Severity = Severity("urgent")
	if _, _, err := svc.Record(context.Background(), input, time.Now().UTC()); err == nil {
		t.Fatal("invalid severity accepted")
	}
	input = validEventInput()
	input.Fingerprint = ""
	if _, _, err := svc.Record(context.Background(), input, time.Now().UTC()); err == nil {
		t.Fatal("empty fingerprint accepted")
	}
}

func TestRecordNotifiesRepeatOnlyAfterCooldown(t *testing.T) {
	notifier := &recordingNotifier{}
	svc, _ := newEventTestService(t, notifier)
	input := validEventInput()
	firstAt := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	if _, _, err := svc.Record(context.Background(), input, firstAt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Record(context.Background(), input, firstAt.Add(14*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Record(context.Background(), input, firstAt.Add(16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(notifier.messages) != 2 {
		t.Fatalf("notifications = %d, want initial and post-cooldown messages", len(notifier.messages))
	}
}

func TestTransitionRejectsInvalidStateChange(t *testing.T) {
	svc, _ := newEventTestService(t, nil)
	event, _, err := svc.Record(context.Background(), validEventInput(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Transition(context.Background(), event.ID, StatusResolved, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := svc.Transition(context.Background(), event.ID, StatusAcknowledged, time.Now().UTC()); err == nil {
		t.Fatal("resolved event reopened")
	}
}

func TestCleanupRetainsOpenEventsAndRemovesOldClosedEvents(t *testing.T) {
	svc, db := newEventTestService(t, nil)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	old := now.Add(-100 * 24 * time.Hour)
	openEvent, _, err := svc.Record(context.Background(), validEventInput(), old)
	if err != nil {
		t.Fatal(err)
	}
	closedInput := validEventInput()
	closedInput.Fingerprint = "malware:old"
	closedInput.Component = "malware"
	closedEvent, _, err := svc.Record(context.Background(), closedInput, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Transition(context.Background(), closedEvent.ID, StatusResolved, old.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cleanup(context.Background(), now); err != nil {
		t.Fatal(err)
	}

	var openCount, closedCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE id = ?`, openEvent.ID).Scan(&openCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE id = ?`, closedEvent.ID).Scan(&closedCount)
	if openCount != 1 || closedCount != 0 {
		t.Fatalf("after cleanup: open=%d closed=%d", openCount, closedCount)
	}
}
