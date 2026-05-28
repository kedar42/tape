package portwatch

import (
	"context"
	"errors"
	"time"
)

type Runner struct {
	cfg            Config
	gluetun        GluetunAPI
	qbit           QbitAPI
	log            *Logger
	state          WatchState
	initialized    bool
	lastQbitRead   time.Time
	revalidateQbit bool
	reacquireDelay time.Duration
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

		timer := time.NewTimer(r.cfg.Interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
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
		r.log.Log(string(ActionRecordMissingPort), map[string]any{"error": err})
		gluetunPort = 0
	}

	if ValidPort(gluetunPort) {
		r.refreshQbitIfNeeded(ctx, now)
	}

	decision := Decide(DecisionInput{
		Now:               now,
		GluetunPort:       gluetunPort,
		State:             r.state,
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
		r.state.MissingPortFailures = decision.NextMissingPortFailures
		fields := map[string]any{"gluetun_port": gluetunPort, "failures": r.state.MissingPortFailures, "threshold": r.cfg.Failures}
		if missingPortReason != "" {
			fields["reason"] = missingPortReason
		}
		r.log.Log(string(ActionRecordMissingPort), fields)
		return nil
	case ActionReacquirePort:
		return r.reacquire(ctx, now)
	case ActionCooldown:
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
	r.state.LastReacquireAt = now
	r.state.MissingPortFailures = 0

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
		timer := time.NewTimer(r.reacquireDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.Join(stopErr, ctx.Err())
		case <-timer.C:
		}
	}

	r.log.Log(string(ActionReacquirePort), map[string]any{"phase": "start", "cooldown": r.cfg.Cooldown})
	startErr := r.gluetun.SetVPNStatus(ctx, "running")
	r.revalidateQbit = true
	return errors.Join(stopErr, startErr)
}
