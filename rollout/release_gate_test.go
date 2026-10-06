package rollout

import (
	"context"
	"testing"
	"time"
)

type stubFlags struct {
	definition FlagDefinition
	enabled    bool
}

func (s stubFlags) Get(context.Context, string) (FlagDefinition, error) {
	return s.definition, nil
}

func (s stubFlags) IsEnabled(context.Context, string) (bool, error) {
	return s.enabled, nil
}

func TestGateDecisions(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		enabled bool
		action  string
		reason  string
	}{
		{name: "passed build and enabled flag promotes", status: "passed", enabled: true, action: "promote", reason: "build passed and feature flag is enabled"},
		{name: "passed build and disabled flag holds", status: "passed", enabled: false, action: "hold", reason: "feature flag is disabled"},
		{name: "failed build holds before flag lookup", status: "failed", enabled: true, action: "hold", reason: "build checks did not pass"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gate := NewGate(stubFlags{
				definition: FlagDefinition{Key: "devtools.symbol-index", DefaultValue: false},
				enabled:    test.enabled,
			})
			gate.now = func() time.Time { return time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC) }

			decision, err := gate.Decide(context.Background(), "devtools.symbol-index", BuildEvent{
				BuildID: "build-1842",
				Commit:  "a13f24c",
				Status:  test.status,
			})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.Action != test.action || decision.Reason != test.reason {
				t.Fatalf("Decide() = action %q, reason %q", decision.Action, decision.Reason)
			}
			if got := len(gate.Diagnostics()); got != 1 {
				t.Fatalf("Diagnostics() count = %d, want 1", got)
			}
		})
	}
}
