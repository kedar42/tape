package portwatch

import "time"

type Action string

const (
	ActionNoop              Action = "noop"
	ActionSyncQbit          Action = "sync_qbit"
	ActionRecordMissingPort Action = "record_missing_port"
	ActionReacquirePort     Action = "reacquire_port"
	ActionCooldown          Action = "cooldown"
)

type DecisionInput struct {
	Now               time.Time
	GluetunPort       int
	State             WatchState
	FailuresThreshold int
	Cooldown          time.Duration
}

type Decision struct {
	Action                  Action
	NextMissingPortFailures int
}

func ValidPort(port int) bool {
	return port >= 1 && port <= 65535
}

func Decide(input DecisionInput) Decision {
	if ValidPort(input.GluetunPort) {
		if input.State.CacheValid && input.State.CachedQbitPort == input.GluetunPort {
			return Decision{Action: ActionNoop}
		}
		return Decision{Action: ActionSyncQbit}
	}

	failures := input.State.MissingPortFailures + 1
	if failures < input.FailuresThreshold {
		return Decision{Action: ActionRecordMissingPort, NextMissingPortFailures: failures}
	}
	if input.State.LastReacquireAt.IsZero() || input.Now.Sub(input.State.LastReacquireAt) >= input.Cooldown {
		return Decision{Action: ActionReacquirePort}
	}
	return Decision{Action: ActionCooldown, NextMissingPortFailures: failures}
}

func (state *WatchState) ApplySyncResult(port int, err error) {
	if err != nil || !ValidPort(port) {
		return
	}
	state.CachedQbitPort = port
	state.CacheValid = true
}
