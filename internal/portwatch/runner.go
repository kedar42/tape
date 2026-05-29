package portwatch

import (
	"context"
	"errors"
	"time"
)

type Runner struct {
	cfg               Config
	gluetun           GluetunAPI
	qbit              QbitAPI
	log               *Logger
	state             WatchState
	initialized       bool
	lastQbitRead      time.Time
	revalidateQbit    bool
	reacquireDelay    time.Duration
	recoveryStartedAt time.Time
}

func NewRunner(cfg Config, gluetun GluetunAPI, qbit QbitAPI, log *Logger) *Runner {
	return &Runner{
		cfg:            cfg,
		gluetun:        gluetun,
		qbit:           qbit,
		log:            log,
		reacquireDelay: 5 * time.Second,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	for {
		if err := r.RunOnce(ctx); err != nil {
			return err
		}
		if r.cfg.Once {
			return nil
		}

		timer := time.NewTimer(r.nextInterval(time.Now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *Runner) RunOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now()
	r.initQbitCache(ctx, now)

	gluetunPort, missingPortReason, err := r.getGluetunPort(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		r.log.Log("gluetun_read_error", map[string]any{"error": err})
		return nil
	}

	if ValidPort(gluetunPort) {
		r.exitRecovery()
		r.refreshQbitIfNeeded(ctx, now)
	}

	decision := Decide(DecisionInput{
		Now:               now,
		GluetunPort:       gluetunPort,
		State:             r.state,
		ForceQbitSync:     r.state.ForceQbitSync,
		FailuresThreshold: r.cfg.Failures,
		Cooldown:          r.cfg.Cooldown,
	})

	switch decision.Action {
	case ActionNoop:
		r.state.MissingPortFailures = 0
		r.log.Log(string(ActionNoop), map[string]any{"gluetun_port": gluetunPort, "qbit_port": r.state.CachedQbitPort})
		return nil
	case ActionSyncQbit:
		return r.syncQbit(ctx, gluetunPort)
	case ActionRecordMissingPort:
		r.enterRecovery(now)
		r.state.MissingPortFailures = decision.NextMissingPortFailures
		fields := map[string]any{"gluetun_port": gluetunPort, "failures": r.state.MissingPortFailures, "threshold": r.cfg.Failures}
		if missingPortReason != "" {
			fields["reason"] = missingPortReason
		}
		r.log.Log(string(ActionRecordMissingPort), fields)
		return nil
	case ActionReacquirePort:
		r.enterRecovery(now)
		return r.reacquire(ctx, now)
	case ActionCooldown:
		r.enterRecovery(now)
		r.state.MissingPortFailures = decision.NextMissingPortFailures
		fields := map[string]any{"gluetun_port": gluetunPort, "failures": r.state.MissingPortFailures, "threshold": r.cfg.Failures}
		if missingPortReason != "" {
			fields["reason"] = missingPortReason
		}
		r.log.Log(string(ActionCooldown), fields)
		return nil
	default:
		return nil
	}
}

func (r *Runner) enterRecovery(now time.Time) {
	if r.recoveryStartedAt.IsZero() {
		r.recoveryStartedAt = now
	}
}

func (r *Runner) exitRecovery() {
	r.recoveryStartedAt = time.Time{}
}

func (r *Runner) nextInterval(now time.Time) time.Duration {
	if r.recoveryStartedAt.IsZero() || now.Sub(r.recoveryStartedAt) >= r.cfg.RecoveryDuration {
		return r.cfg.Interval
	}
	return r.cfg.RecoveryInterval
}

func (r *Runner) getGluetunPort(ctx context.Context) (int, string, error) {
	if gluetun, ok := r.gluetun.(GluetunPortStatusAPI); ok {
		status, err := gluetun.GetForwardedPortStatus(ctx)
		return status.Port, status.Reason, err
	}

	port, err := r.gluetun.GetForwardedPort(ctx)
	return port, portReason(port), err
}

func portReason(port int) string {
	if port == 0 {
		return "zero_port"
	}
	return ""
}

func (r *Runner) initQbitCache(ctx context.Context, now time.Time) {
	if r.initialized {
		return
	}
	r.initialized = true

	port, safe, err := r.readQbitCache(ctx)
	if err != nil {
		r.log.Log("init_qbit_cache", map[string]any{"error": err})
		return
	}
	r.state.CachedQbitPort = port
	r.state.CacheValid = safe
	r.lastQbitRead = now
	r.log.Log("init_qbit_cache", map[string]any{"qbit_port": port, "cache_valid": r.state.CacheValid})
}

func (r *Runner) refreshQbitIfNeeded(ctx context.Context, now time.Time) {
	if !r.revalidateQbit && r.state.CacheValid && !r.lastQbitRead.IsZero() && now.Sub(r.lastQbitRead) < r.cfg.QbitAuditInterval {
		return
	}

	port, safe, err := r.readQbitCache(ctx)
	if err != nil {
		r.log.Log("audit_qbit", map[string]any{"error": err, "cache_valid": r.state.CacheValid})
		return
	}
	r.state.CachedQbitPort = port
	r.state.CacheValid = safe
	r.lastQbitRead = now
	r.revalidateQbit = false
	r.log.Log("audit_qbit", map[string]any{"qbit_port": port, "cache_valid": r.state.CacheValid})
}

func (r *Runner) readQbitCache(ctx context.Context) (int, bool, error) {
	if qbit, ok := r.qbit.(QbitPreferencesAPI); ok {
		prefs, err := qbit.GetPreferences(ctx)
		if err != nil {
			return 0, false, err
		}
		return prefs.ListenPort, qbitPreferencesSafe(prefs, r.cfg.QbitInterface), nil
	}

	port, err := r.qbit.GetListenPort(ctx)
	if err != nil {
		return 0, false, err
	}
	return port, ValidPort(port), nil
}

func qbitPreferencesSafe(prefs QbitPreferences, iface string) bool {
	return ValidPort(prefs.ListenPort) && prefs.CurrentNetworkInterface == iface && !prefs.RandomPort && !prefs.UPnP
}

func (r *Runner) syncQbit(ctx context.Context, gluetunPort int) error {
	if !ValidPort(gluetunPort) {
		return nil
	}

	fields := map[string]any{"gluetun_port": gluetunPort, "qbit_port": r.state.CachedQbitPort, "cache_valid": r.state.CacheValid}
	if r.cfg.DryRun {
		fields["dry_run"] = true
		r.log.Log(string(ActionSyncQbit), fields)
		return nil
	}

	err := r.qbit.SetListenPort(ctx, gluetunPort, r.cfg.QbitInterface)
	r.state.ApplySyncResult(gluetunPort, err)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		r.state.CacheValid = false
		r.revalidateQbit = true
		fields["error"] = err
		r.log.Log(string(ActionSyncQbit), fields)
		return nil
	}
	r.state.MissingPortFailures = 0
	fields["qbit_port"] = r.state.CachedQbitPort
	fields["cache_valid"] = r.state.CacheValid
	r.log.Log(string(ActionSyncQbit), fields)
	return nil
}

func (r *Runner) reacquire(ctx context.Context, now time.Time) error {
	if r.cfg.DryRun {
		r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "stop", "dry_run": true})
		r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "start", "dry_run": true})
		return nil
	}

	r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "stop"})
	stopErr := r.gluetun.SetVPNStatus(ctx, "stopped")
	if err := ctx.Err(); err != nil {
		return errors.Join(stopErr, err)
	}
	if r.reacquireDelay > 0 {
		if err := sleepContext(ctx, r.reacquireDelay); err != nil {
			return errors.Join(stopErr, err)
		}
	}

	r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "start", "cooldown": r.cfg.Cooldown})
	startErr := r.gluetun.SetVPNStatus(ctx, "running")
	if err := ctx.Err(); err != nil {
		return errors.Join(stopErr, startErr, err)
	}
	if startErr != nil {
		r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "error", "stop_error": stopErr, "start_error": startErr})
		return errors.Join(stopErr, startErr)
	}
	if stopErr != nil {
		r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "error", "stop_error": stopErr})
		return nil
	}

	r.commitReacquireSuccess(now)
	return nil
}

func (r *Runner) commitReacquireSuccess(now time.Time) {
	r.state.LastReacquireAt = now
	r.state.MissingPortFailures = 0
	r.state.ForceQbitSync = true
	r.revalidateQbit = true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
