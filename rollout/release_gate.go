package rollout

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type FlagReader interface {
	Get(context.Context, string) (FlagDefinition, error)
	IsEnabled(context.Context, string) (bool, error)
}

type FlagWriter interface {
	Set(context.Context, string, bool, string) error
	Rollout(context.Context, string, int, string) error
}

type BuildEvent struct {
	BuildID string `json:"build_id"`
	Commit  string `json:"commit"`
	Status  string `json:"status"`
}

type BuildDecision struct {
	BuildID     string `json:"build_id"`
	Commit      string `json:"commit"`
	FlagKey     string `json:"flag_key"`
	Default     bool   `json:"default_value"`
	FlagEnabled bool   `json:"flag_enabled"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	DecidedAt   string `json:"decided_at"`
}

type ReleaseOperation struct {
	OperationID string `json:"operation_id"`
	FlagKey     string `json:"flag_key"`
	Percentage  int    `json:"percentage"`
}

type Gate struct {
	flags FlagReader
	now   func() time.Time
	mu    sync.RWMutex
	log   []BuildDecision
}

func NewGate(flags FlagReader) *Gate {
	return &Gate{flags: flags, now: time.Now}
}

func (g *Gate) Decide(ctx context.Context, flagKey string, event BuildEvent) (BuildDecision, error) {
	decision := BuildDecision{
		BuildID:   event.BuildID,
		Commit:    event.Commit,
		FlagKey:   flagKey,
		Action:    "hold",
		DecidedAt: g.now().UTC().Format(time.RFC3339),
	}
	if event.BuildID == "" || event.Commit == "" {
		return decision, fmt.Errorf("build_id and commit are required")
	}
	if event.Status != "passed" {
		decision.Reason = "build checks did not pass"
		g.record(decision)
		return decision, nil
	}

	definition, err := g.flags.Get(ctx, flagKey)
	if err != nil {
		return decision, err
	}
	decision.Default = definition.DefaultValue
	enabled, err := g.flags.IsEnabled(ctx, flagKey)
	if err != nil {
		return decision, err
	}
	decision.FlagEnabled = enabled
	if enabled {
		decision.Action = "promote"
		decision.Reason = "build passed and feature flag is enabled"
	} else {
		decision.Reason = "feature flag is disabled"
	}
	g.record(decision)
	return decision, nil
}

func ApplyRelease(ctx context.Context, flags FlagWriter, operation ReleaseOperation) error {
	if operation.OperationID == "" || operation.FlagKey == "" {
		return fmt.Errorf("operation_id and flag_key are required")
	}
	if operation.Percentage < 0 || operation.Percentage > 100 {
		return fmt.Errorf("percentage must be between 0 and 100")
	}
	if err := flags.Set(ctx, operation.FlagKey, false, operation.OperationID+":set"); err != nil {
		return err
	}
	return flags.Rollout(ctx, operation.FlagKey, operation.Percentage, operation.OperationID+":rollout")
}

func (g *Gate) Diagnostics() []BuildDecision {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]BuildDecision, len(g.log))
	copy(result, g.log)
	return result
}

func (g *Gate) record(decision BuildDecision) {
	g.mu.Lock()
	defer g.mu.Unlock()
	const retained = 50
	g.log = append(g.log, decision)
	if len(g.log) > retained {
		g.log = append([]BuildDecision(nil), g.log[len(g.log)-retained:]...)
	}
}
