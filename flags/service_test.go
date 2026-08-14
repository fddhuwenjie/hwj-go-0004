package flags_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fddhuwenjie/hwj-go-0004/flags"
)

func mustUpsert(t *testing.T, service *flags.Service, flag flags.Flag) flags.Flag {
	t.Helper()
	stored, err := service.Upsert(flag, "")
	if err != nil {
		t.Fatalf("upsert %s: %v", flag.Key, err)
	}
	return stored
}

func TestRulePriorityRolloutAndIsolation(t *testing.T) {
	service := flags.NewService()
	stored := mustUpsert(t, service, flags.Flag{
		Key: "checkout", Enabled: true, Default: "off",
		Rules: []flags.Rule{
			{ID: "country", Priority: 20, Value: "country", Conditions: []flags.Condition{{Attribute: "country", Operator: "equals", Values: []string{"US"}}}},
			{ID: "staff", Priority: 10, Value: "staff", Conditions: []flags.Condition{{Attribute: "role", Operator: "equals", Values: []string{"staff"}}}},
		},
		Rollout: []flags.RolloutOption{{Value: "on", Weight: 5000}, {Value: "off", Weight: 5000}},
	})
	if stored.Version != 1 {
		t.Fatalf("version = %d, want 1", stored.Version)
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	evaluation, err := service.Evaluate("checkout", flags.Context{SubjectKey: "alice", Attributes: map[string]string{"role": "staff", "country": "US"}}, now)
	if err != nil || evaluation.Value != "staff" || evaluation.Reason != "rule:staff" {
		t.Fatalf("priority evaluation = %+v err=%v", evaluation, err)
	}
	first, err := service.Evaluate("checkout", flags.Context{SubjectKey: "stable-user"}, now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		next, err := service.Evaluate("checkout", flags.Context{SubjectKey: "stable-user"}, now.Add(time.Duration(i)*time.Minute))
		if err != nil || next.Value != first.Value {
			t.Fatalf("rollout changed at %d: %+v err=%v", i, next, err)
		}
	}
	copy, err := service.Get("checkout")
	if err != nil {
		t.Fatal(err)
	}
	copy.Rules[0].Conditions[0].Values[0] = "changed"
	again, _ := service.Get("checkout")
	if again.Rules[0].Conditions[0].Values[0] == "changed" {
		t.Fatal("Get returned mutable internal state")
	}
}

func TestSchedulePrerequisitesAndCycle(t *testing.T) {
	service := flags.NewService()
	mustUpsert(t, service, flags.Flag{Key: "account", Enabled: true, Default: "free", Rules: []flags.Rule{{ID: "pro", Priority: 1, Value: "pro", Conditions: []flags.Condition{{Attribute: "plan", Operator: "equals", Values: []string{"pro"}}}}}})
	mustUpsert(t, service, flags.Flag{
		Key: "launch", Enabled: true, Default: "off",
		Prerequisites: []flags.Prerequisite{{Key: "account", Value: "pro"}},
		Schedule:      &flags.Schedule{Start: "2026-08-15T09:00:00", End: "2026-08-15T17:00:00", Location: "America/New_York"},
		Rules:         []flags.Rule{{ID: "everyone", Priority: 1, Value: "on", Conditions: []flags.Condition{{Attribute: "region", Operator: "not_equals", Values: []string{"blocked"}}}}},
	})
	inside := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	result, err := service.Evaluate("launch", flags.Context{SubjectKey: "u1", Attributes: map[string]string{"plan": "pro", "region": "open"}}, inside)
	if err != nil || result.Value != "on" {
		t.Fatalf("inside schedule = %+v err=%v", result, err)
	}
	outside, err := service.Evaluate("launch", flags.Context{SubjectKey: "u1", Attributes: map[string]string{"plan": "pro", "region": "open"}}, inside.Add(10*time.Hour))
	if err != nil || outside.Reason != "outside_schedule" {
		t.Fatalf("outside schedule = %+v err=%v", outside, err)
	}
	mustUpsert(t, service, flags.Flag{Key: "a", Enabled: true, Default: "off", Prerequisites: []flags.Prerequisite{{Key: "b", Value: "on"}}})
	mustUpsert(t, service, flags.Flag{Key: "b", Enabled: true, Default: "off", Prerequisites: []flags.Prerequisite{{Key: "a", Value: "on"}}})
	if _, err := service.Evaluate("a", flags.Context{SubjectKey: "u"}, inside); !errors.Is(err, flags.ErrPrerequisiteCycle) {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestImportAtomicityAndRequestIdempotency(t *testing.T) {
	service := flags.NewService()
	batch := []flags.Flag{{Key: "one", Enabled: true, Default: "off"}, {Key: "two", Enabled: true, Default: "off"}}
	first, err := service.Import(batch, "import-1")
	if err != nil || len(first) != 2 {
		t.Fatalf("import = %+v err=%v", first, err)
	}
	retry, err := service.Import(batch, "import-1")
	if err != nil || retry[0].Version != first[0].Version {
		t.Fatalf("retry = %+v err=%v", retry, err)
	}
	changed := []flags.Flag{{Key: "one", Enabled: true, Default: "on"}, {Key: "two", Enabled: true, Default: "off"}}
	if _, err := service.Import(changed, "import-1"); !errors.Is(err, flags.ErrRequestConflict) {
		t.Fatalf("changed retry error = %v", err)
	}
	bad := []flags.Flag{{Key: "three", Enabled: true, Default: "off"}, {Key: "bad", Enabled: true}}
	if _, err := service.Import(bad, ""); !errors.Is(err, flags.ErrInvalidInput) {
		t.Fatalf("invalid import error = %v", err)
	}
	if _, err := service.Get("three"); !errors.Is(err, flags.ErrFlagNotFound) {
		t.Fatalf("partial import persisted: %v", err)
	}
}

func TestUpsertRequestConflictOnChangedPayload(t *testing.T) {
	service := flags.NewService()
	first, err := service.Upsert(flags.Flag{Key: "checkout", Enabled: true, Default: "off"}, "upsert-1")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.Upsert(flags.Flag{Key: "checkout", Enabled: true, Default: "off"}, "upsert-1")
	if err != nil || retry.Version != first.Version {
		t.Fatalf("retry = %+v err=%v", retry, err)
	}
	if _, err := service.Upsert(flags.Flag{Key: "checkout", Enabled: true, Default: "on"}, "upsert-1"); !errors.Is(err, flags.ErrRequestConflict) {
		t.Fatalf("changed retry error = %v", err)
	}
}

func TestAuditAndConcurrentEvaluation(t *testing.T) {
	service := flags.NewService()
	mustUpsert(t, service, flags.Flag{Key: "search", Enabled: true, Default: "off", Rollout: []flags.RolloutOption{{Value: "on", Weight: 5000}, {Value: "off", Weight: 5000}}})
	base := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	const count = 100
	var wait sync.WaitGroup
	wait.Add(count)
	for i := 0; i < count; i++ {
		i := i
		go func() {
			defer wait.Done()
			if _, err := service.Evaluate("search", flags.Context{SubjectKey: fmt.Sprintf("u-%03d", i)}, base.Add(time.Duration(i)*time.Second)); err != nil {
				t.Errorf("evaluate %d: %v", i, err)
			}
		}()
	}
	wait.Wait()
	events, err := service.QueryAudit(flags.AuditQuery{FlagKey: "search", From: base, To: base.Add(50 * time.Second)})
	if err != nil || len(events) != 50 {
		t.Fatalf("audit count = %d err=%v", len(events), err)
	}
	limited, err := service.QueryAudit(flags.AuditQuery{FlagKey: "search", Limit: 3})
	if err != nil || len(limited) != 3 {
		t.Fatalf("limited audit = %d err=%v", len(limited), err)
	}
}

func TestValidation(t *testing.T) {
	service := flags.NewService()
	cases := []flags.Flag{
		{Key: "", Enabled: true, Default: "off"},
		{Key: "x", Enabled: true, Default: "off", Rollout: []flags.RolloutOption{{Value: "on", Weight: 10}}},
		{Key: "x", Enabled: true, Default: "off", Prerequisites: []flags.Prerequisite{{Key: "x", Value: "on"}}},
	}
	for _, flag := range cases {
		if _, err := service.Upsert(flag, ""); !errors.Is(err, flags.ErrInvalidInput) {
			t.Errorf("flag %+v: %v", flag, err)
		}
	}
}
