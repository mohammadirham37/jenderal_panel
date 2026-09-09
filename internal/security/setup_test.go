package security

import (
	"context"
	"reflect"
	"testing"
)

func TestReviewListsEveryMutationBeforeApply(t *testing.T) {
	svc, _ := setupHarness(t)
	review, err := svc.Review(context.Background(), SetupRequest{ManagementCIDRs: []string{"203.0.113.10/32"}, EnableFail2ban: true, MalwareMode: "low_memory", ScheduleMalware: true, ScheduleTime: "02:00", TrafficWebsiteIDs: []string{"site"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"install fail2ban", "configure sshd jail", "install clamav (low_memory)", "update clamav signatures", "schedule daily quick scan", "start Traffic Guard observation for 1 website(s)"}
	if !reflect.DeepEqual(want, review.Mutations) {
		t.Fatalf("mutations=%v", review.Mutations)
	}
}

func TestApplyResumesAfterCompletedStepWithoutRepeatingIt(t *testing.T) {
	svc, calls := setupHarness(t)
	request := SetupRequest{ManagementCIDRs: []string{"203.0.113.10/32"}, EnableFail2ban: true, MalwareMode: "low_memory"}
	review, err := svc.Review(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateRun(context.Background(), request, review)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.markStep(context.Background(), run.ID, "fail2ban_install", nil); err != nil {
		t.Fatal(err)
	}
	if err = svc.Execute(context.Background(), run.ID, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if calls.fail2banInstall != 0 || calls.malwareInstall != 1 {
		t.Fatalf("calls=%#v", calls)
	}
}

type setupCalls struct{ fail2banInstall, malwareInstall int }

func setupHarness(t *testing.T) (*SetupService, *setupCalls) {
	_, db := newEventTestService(t, nil)
	calls := &setupCalls{}
	actions := SetupActions{
		InstallFail2ban:   func(context.Context, func(string)) error { calls.fail2banInstall++; return nil },
		ConfigureFail2ban: func(context.Context, []string, func(string)) error { return nil },
		InstallMalware:    func(context.Context, string, func(string)) error { calls.malwareInstall++; return nil },
		UpdateSignatures:  func(context.Context, func(string)) error { return nil },
		ScheduleMalware:   func(context.Context, string) error { return nil },
		ObserveTraffic:    func(context.Context, []string, func(string)) error { return nil },
	}
	return NewSetupService(db, actions, nil), calls
}
