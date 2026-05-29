package portwatch

import (
	"errors"
	"testing"
	"time"
)

func TestDecideMatchingValidPortsNoops(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	decision := Decide(DecisionInput{
		Now:         now,
		GluetunPort: 51820,
		State: WatchState{
			CachedQbitPort:      51820,
			CacheValid:          true,
			MissingPortFailures: 2,
		},
		FailuresThreshold: 3,
		Cooldown:          5 * time.Minute,
	})

	if decision.Action != ActionNoop {
		t.Fatalf("Action = %v, want %v", decision.Action, ActionNoop)
	}
	if decision.NextMissingPortFailures != 0 {
		t.Fatalf("NextMissingPortFailures = %d, want 0", decision.NextMissingPortFailures)
	}
}

func TestDecideMatchingPortSyncsWhenForceQbitSync(t *testing.T) {
	decision := Decide(DecisionInput{
		Now:         time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
		GluetunPort: 51820,
		State: WatchState{
			CachedQbitPort: 51820,
			CacheValid:     true,
			ForceQbitSync:  true,
		},
		FailuresThreshold: 3,
		Cooldown:          5 * time.Minute,
	})

	if decision.Action != ActionSyncQbit {
		t.Fatalf("Action = %v, want %v", decision.Action, ActionSyncQbit)
	}
	if decision.NextMissingPortFailures != 0 {
		t.Fatalf("NextMissingPortFailures = %d, want 0", decision.NextMissingPortFailures)
	}
}

func TestDecideValidGluetunPortSyncsQbit(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		state WatchState
	}{
		{
			name: "different cached qbit port",
			state: WatchState{
				CachedQbitPort:      6881,
				CacheValid:          true,
				MissingPortFailures: 2,
			},
		},
		{
			name: "unknown cache",
			state: WatchState{
				MissingPortFailures: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := Decide(DecisionInput{
				Now:               now,
				GluetunPort:       51820,
				State:             tt.state,
				FailuresThreshold: 3,
				Cooldown:          5 * time.Minute,
			})

			if decision.Action != ActionSyncQbit {
				t.Fatalf("Action = %v, want %v", decision.Action, ActionSyncQbit)
			}
			if decision.NextMissingPortFailures != 0 {
				t.Fatalf("NextMissingPortFailures = %d, want 0", decision.NextMissingPortFailures)
			}
		})
	}
}

func TestMissingPortBelowThresholdRecordsFailure(t *testing.T) {
	decision := Decide(DecisionInput{
		Now:         time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
		GluetunPort: 0,
		State: WatchState{
			MissingPortFailures: 1,
		},
		FailuresThreshold: 3,
		Cooldown:          5 * time.Minute,
	})

	if decision.Action != ActionRecordMissingPort {
		t.Fatalf("Action = %v, want %v", decision.Action, ActionRecordMissingPort)
	}
	if decision.NextMissingPortFailures != 2 {
		t.Fatalf("NextMissingPortFailures = %d, want 2", decision.NextMissingPortFailures)
	}
}

func TestMissingPortAtThresholdReacquires(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	decision := Decide(DecisionInput{
		Now:         now,
		GluetunPort: 0,
		State: WatchState{
			MissingPortFailures: 2,
			LastReacquireAt:     now.Add(-10 * time.Minute),
		},
		FailuresThreshold: 3,
		Cooldown:          5 * time.Minute,
	})

	if decision.Action != ActionReacquirePort {
		t.Fatalf("Action = %v, want %v", decision.Action, ActionReacquirePort)
	}
	if decision.NextMissingPortFailures != 0 {
		t.Fatalf("NextMissingPortFailures = %d, want 0", decision.NextMissingPortFailures)
	}
}

func TestCooldownSuppressesRepeatedReacquire(t *testing.T) {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)

	decision := Decide(DecisionInput{
		Now:         now,
		GluetunPort: 0,
		State: WatchState{
			MissingPortFailures: 2,
			LastReacquireAt:     now.Add(-time.Minute),
		},
		FailuresThreshold: 3,
		Cooldown:          5 * time.Minute,
	})

	if decision.Action != ActionCooldown {
		t.Fatalf("Action = %v, want %v", decision.Action, ActionCooldown)
	}
	if decision.NextMissingPortFailures != 3 {
		t.Fatalf("NextMissingPortFailures = %d, want 3", decision.NextMissingPortFailures)
	}
}

func TestApplySyncResultUpdatesCacheOnSuccessfulValidPort(t *testing.T) {
	state := WatchState{}

	state.ApplySyncResult(51820, nil)

	if state.CachedQbitPort != 51820 {
		t.Fatalf("CachedQbitPort = %d, want 51820", state.CachedQbitPort)
	}
	if !state.CacheValid {
		t.Fatal("CacheValid = false, want true")
	}
}

func TestApplySyncResultDoesNotUpdateCacheOnFailureOrInvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
		err  error
	}{
		{name: "failed sync", port: 51820, err: errors.New("write failed")},
		{name: "invalid sync port", port: 0, err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := WatchState{CachedQbitPort: 6881, CacheValid: true}

			state.ApplySyncResult(tt.port, tt.err)

			if state.CachedQbitPort != 6881 {
				t.Fatalf("CachedQbitPort = %d, want 6881", state.CachedQbitPort)
			}
			if !state.CacheValid {
				t.Fatal("CacheValid = false, want true")
			}
		})
	}
}

func TestValidPort(t *testing.T) {
	tests := []struct {
		port int
		want bool
	}{
		{port: 1, want: true},
		{port: 65535, want: true},
		{port: 0, want: false},
		{port: -1, want: false},
		{port: 65536, want: false},
	}

	for _, tt := range tests {
		if got := ValidPort(tt.port); got != tt.want {
			t.Fatalf("ValidPort(%d) = %v, want %v", tt.port, got, tt.want)
		}
	}
}
