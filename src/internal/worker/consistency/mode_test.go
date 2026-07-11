package consistency

import (
	"path/filepath"
	"testing"
	"time"

	"plasmod/src/internal/schemas"
)

func TestParseModeAcceptsCanonicalNamesAndAliases(t *testing.T) {
	t.Parallel()

	tests := map[string]Mode{
		"strict_visible":      StrictVisible,
		"strict":              StrictVisible,
		"bounded_staleness":   BoundedStaleness,
		"bounded":             BoundedStaleness,
		"eventual_visibility": EventualVisibility,
		"eventual":            EventualVisibility,
	}
	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseMode(input)
			if err != nil {
				t.Fatalf("ParseMode(%q): %v", input, err)
			}
			if got != want {
				t.Fatalf("ParseMode(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func TestParseModeRejectsUnsupportedValue(t *testing.T) {
	t.Parallel()

	if _, err := ParseMode("linearizable-ish"); err == nil {
		t.Fatal("expected unsupported mode error")
	}
}

func TestResolveWriteExplicitModeOverridesDefaultAndPersistsCanonicalValue(t *testing.T) {
	t.Parallel()

	slaMS := int64(250)
	ev := schemas.Event{Access: schemas.EventAccess{
		Consistency:    "bounded",
		FreshnessSLAMS: &slaMS,
	}}

	mode, lag, normalized, err := ResolveWrite(StrictVisible, ev, time.Second)
	if err != nil {
		t.Fatalf("ResolveWrite: %v", err)
	}
	if mode != BoundedStaleness {
		t.Fatalf("mode = %q, want %q", mode, BoundedStaleness)
	}
	if lag != 250*time.Millisecond {
		t.Fatalf("lag = %v, want 250ms", lag)
	}
	if normalized.Access.Consistency != string(BoundedStaleness) {
		t.Fatalf("persisted mode = %q, want %q", normalized.Access.Consistency, BoundedStaleness)
	}
	if normalized.Access.FreshnessSLAMS == nil || *normalized.Access.FreshnessSLAMS != 250 {
		t.Fatalf("persisted SLA = %v, want 250", normalized.Access.FreshnessSLAMS)
	}
}

func TestResolveWriteUsesDefaultModeAndBoundedLag(t *testing.T) {
	t.Parallel()

	mode, lag, normalized, err := ResolveWrite(BoundedStaleness, schemas.Event{}, time.Second)
	if err != nil {
		t.Fatalf("ResolveWrite: %v", err)
	}
	if mode != BoundedStaleness || lag != time.Second {
		t.Fatalf("got mode=%q lag=%v, want bounded_staleness/1s", mode, lag)
	}
	if normalized.Access.Consistency != string(BoundedStaleness) {
		t.Fatalf("persisted mode = %q", normalized.Access.Consistency)
	}
	if normalized.Access.FreshnessSLAMS == nil || *normalized.Access.FreshnessSLAMS != 1000 {
		t.Fatalf("persisted SLA = %v, want 1000", normalized.Access.FreshnessSLAMS)
	}
}

func TestResolveWriteRejectsInvalidExplicitModeAndSLA(t *testing.T) {
	t.Parallel()

	badSLA := int64(0)
	tests := []schemas.Event{
		{Access: schemas.EventAccess{Consistency: "unsupported"}},
		{Access: schemas.EventAccess{Consistency: "bounded", FreshnessSLAMS: &badSLA}},
	}
	for _, ev := range tests {
		if _, _, _, err := ResolveWrite(StrictVisible, ev, time.Second); err == nil {
			t.Fatalf("expected error for access=%+v", ev.Access)
		}
	}
}

func TestResolveReadUsesQueryOverrideThenDefault(t *testing.T) {
	t.Parallel()

	mode, err := ResolveRead(StrictVisible, schemas.QueryRequest{AccessConsistency: "eventual"})
	if err != nil {
		t.Fatalf("ResolveRead override: %v", err)
	}
	if mode != EventualVisibility {
		t.Fatalf("override mode = %q, want %q", mode, EventualVisibility)
	}

	mode, err = ResolveRead(BoundedStaleness, schemas.QueryRequest{})
	if err != nil {
		t.Fatalf("ResolveRead default: %v", err)
	}
	if mode != BoundedStaleness {
		t.Fatalf("default mode = %q, want %q", mode, BoundedStaleness)
	}
}

func TestConfigFromEnvUsesDefaultsAndPersistentCheckpoint(t *testing.T) {
	dataDir := t.TempDir()
	cfg := ConfigFromEnv(dataDir, true)

	if cfg.DefaultMode != StrictVisible {
		t.Fatalf("default mode = %q, want %q", cfg.DefaultMode, StrictVisible)
	}
	if cfg.BoundedMaxLag != time.Second {
		t.Fatalf("bounded lag = %v, want 1s", cfg.BoundedMaxLag)
	}
	if cfg.QueueSize <= 0 || cfg.Workers <= 0 || cfg.MaxRetries <= 0 {
		t.Fatalf("invalid positive defaults: %+v", cfg)
	}
	if cfg.CheckpointPath != filepath.Join(dataDir, "consistency_checkpoint.json") {
		t.Fatalf("checkpoint path = %q", cfg.CheckpointPath)
	}
	if !cfg.BootstrapCheckpointAtLatest {
		t.Fatal("persistent runtime should bootstrap legacy checkpoint at WAL tail")
	}
}

func TestConfigFromEnvAppliesValidOverridesAndRejectsInvalidOnes(t *testing.T) {
	t.Setenv("PLASMOD_CONSISTENCY_DEFAULT_MODE", "eventual")
	t.Setenv("PLASMOD_CONSISTENCY_BOUNDED_MAX_LAG", "750ms")
	t.Setenv("PLASMOD_CONSISTENCY_QUEUE_SIZE", "128")
	t.Setenv("PLASMOD_CONSISTENCY_WORKERS", "3")
	t.Setenv("PLASMOD_CONSISTENCY_MAX_RETRIES", "5")
	t.Setenv("PLASMOD_CONSISTENCY_RETRY_BASE", "10ms")
	t.Setenv("PLASMOD_CONSISTENCY_RETRY_MAX", "500ms")
	t.Setenv("PLASMOD_CONSISTENCY_QUERY_TIMEOUT", "4s")
	t.Setenv("PLASMOD_CONSISTENCY_SHUTDOWN_TIMEOUT", "6s")
	t.Setenv("PLASMOD_CONSISTENCY_CHECKPOINT_PATH", "/tmp/custom-consistency-checkpoint.json")

	cfg := ConfigFromEnv(t.TempDir(), true)
	if cfg.DefaultMode != EventualVisibility || cfg.BoundedMaxLag != 750*time.Millisecond {
		t.Fatalf("mode/lag overrides not applied: %+v", cfg)
	}
	if cfg.QueueSize != 128 || cfg.Workers != 3 || cfg.MaxRetries != 5 {
		t.Fatalf("integer overrides not applied: %+v", cfg)
	}
	if cfg.RetryBaseDelay != 10*time.Millisecond || cfg.RetryMaxDelay != 500*time.Millisecond {
		t.Fatalf("retry overrides not applied: %+v", cfg)
	}
	if cfg.QueryWaitTimeout != 4*time.Second || cfg.ShutdownTimeout != 6*time.Second {
		t.Fatalf("timeout overrides not applied: %+v", cfg)
	}
	if cfg.CheckpointPath != "/tmp/custom-consistency-checkpoint.json" {
		t.Fatalf("checkpoint override = %q", cfg.CheckpointPath)
	}

	t.Setenv("PLASMOD_CONSISTENCY_DEFAULT_MODE", "not-a-mode")
	t.Setenv("PLASMOD_CONSISTENCY_QUEUE_SIZE", "0")
	t.Setenv("PLASMOD_CONSISTENCY_RETRY_MAX", "1ms")
	fallback := ConfigFromEnv("", false)
	if fallback.DefaultMode != StrictVisible || fallback.QueueSize <= 0 {
		t.Fatalf("invalid values should use safe defaults: %+v", fallback)
	}
	if fallback.RetryMaxDelay < fallback.RetryBaseDelay {
		t.Fatalf("retry max must not be below retry base: %+v", fallback)
	}
	if fallback.CheckpointPath != "" || fallback.BootstrapCheckpointAtLatest {
		t.Fatalf("memory runtime must not persist checkpoints: %+v", fallback)
	}
}
