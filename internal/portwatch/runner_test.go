package portwatch

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeGluetun struct {
	ports        []int
	portStatuses []PortStatus
	getPortCalls int
	statuses     []string
	statusErrs   map[string]error
}

func (f *fakeGluetun) GetForwardedPort(context.Context) (int, error) {
	if f.getPortCalls >= len(f.ports) {
		return 0, errors.New("no fake gluetun port queued")
	}
	port := f.ports[f.getPortCalls]
	f.getPortCalls++
	return port, nil
}

func (f *fakeGluetun) GetForwardedPortStatus(context.Context) (PortStatus, error) {
	if len(f.portStatuses) == 0 {
		port, err := f.GetForwardedPort(context.Background())
		return PortStatus{Port: port, Reason: portReason(port)}, err
	}
	if f.getPortCalls >= len(f.portStatuses) {
		return PortStatus{}, errors.New("no fake gluetun port status queued")
	}
	status := f.portStatuses[f.getPortCalls]
	f.getPortCalls++
	return status, nil
}

func (f *fakeGluetun) SetVPNStatus(_ context.Context, status string) error {
	f.statuses = append(f.statuses, status)
	if f.statusErrs != nil {
		return f.statusErrs[status]
	}
	return nil
}

type fakeQbit struct {
	listenPort int
	prefs      QbitPreferences
	getErr     error
	setErr     error
	getCalls   int
	setCalls   []int
	ifaces     []string
	onSet      func()
}

func (f *fakeQbit) GetListenPort(context.Context) (int, error) {
	f.getCalls++
	if f.getErr != nil {
		return 0, f.getErr
	}
	return f.listenPort, nil
}

func (f *fakeQbit) GetPreferences(context.Context) (QbitPreferences, error) {
	f.getCalls++
	if f.getErr != nil {
		return QbitPreferences{}, f.getErr
	}
	if f.prefs.ListenPort != 0 || f.prefs.CurrentNetworkInterface != "" || f.prefs.RandomPort || f.prefs.UPnP {
		return f.prefs, nil
	}
	return QbitPreferences{
		ListenPort:              f.listenPort,
		CurrentNetworkInterface: "tun0",
		RandomPort:              false,
		UPnP:                    false,
	}, nil
}

func (f *fakeQbit) SetListenPort(_ context.Context, port int, iface string) error {
	f.setCalls = append(f.setCalls, port)
	f.ifaces = append(f.ifaces, iface)
	if f.onSet != nil {
		f.onSet()
	}
	if f.setErr != nil {
		return f.setErr
	}
	f.listenPort = port
	f.prefs = QbitPreferences{
		ListenPort:              port,
		CurrentNetworkInterface: iface,
		RandomPort:              false,
		UPnP:                    false,
	}
	return nil
}

func testRunnerConfig() Config {
	return Config{
		Name:              "test",
		Interval:          time.Hour,
		Failures:          2,
		Cooldown:          time.Minute,
		QbitAuditInterval: time.Hour,
		QbitInterface:     "tun0",
		Once:              true,
	}
}

func TestRunnerStartupReadsQbitOnceIntoCache(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{12345}}
	qbit := &fakeQbit{listenPort: 12345}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 1 {
		t.Fatalf("qbit get calls = %d, want 1", qbit.getCalls)
	}
	if !runner.state.CacheValid || runner.state.CachedQbitPort != 12345 {
		t.Fatalf("cache = %+v, want valid 12345", runner.state)
	}
}

func TestRunnerMatchingCycleDoesNotReadQbitAgain(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{12345, 12345}}
	qbit := &fakeQbit{listenPort: 12345}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 1 {
		t.Fatalf("qbit get calls = %d, want 1", qbit.getCalls)
	}
}

func TestRunnerDifferentGluetunPortWritesQbitAndUpdatesCache(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{listenPort: 11111}
	cfg := testRunnerConfig()
	cfg.QbitInterface = "wg0"
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if len(qbit.setCalls) != 1 || qbit.setCalls[0] != 22222 || qbit.ifaces[0] != "wg0" {
		t.Fatalf("set calls = %v ifaces=%v, want 22222 on wg0", qbit.setCalls, qbit.ifaces)
	}
	if !runner.state.CacheValid || runner.state.CachedQbitPort != 22222 {
		t.Fatalf("cache = %+v, want valid 22222", runner.state)
	}
}

func TestRunnerSyncsWhenPortMatchesButQbitInterfaceUnsafe(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{prefs: QbitPreferences{
		ListenPort:              22222,
		CurrentNetworkInterface: "eth0",
		RandomPort:              false,
		UPnP:                    false,
	}}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if len(qbit.setCalls) != 1 || qbit.setCalls[0] != 22222 || qbit.ifaces[0] != "tun0" {
		t.Fatalf("set calls = %v ifaces=%v, want 22222 on tun0", qbit.setCalls, qbit.ifaces)
	}
}

func TestRunnerSyncsWhenPortMatchesButRandomPortOrUPnPEnabled(t *testing.T) {
	tests := []struct {
		name       string
		randomPort bool
		upnp       bool
	}{
		{name: "random_port", randomPort: true},
		{name: "upnp", upnp: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gluetun := &fakeGluetun{ports: []int{22222}}
			qbit := &fakeQbit{prefs: QbitPreferences{
				ListenPort:              22222,
				CurrentNetworkInterface: "tun0",
				RandomPort:              tt.randomPort,
				UPnP:                    tt.upnp,
			}}
			runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

			if err := runner.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce returned error: %v", err)
			}
			if len(qbit.setCalls) != 1 || qbit.setCalls[0] != 22222 || qbit.ifaces[0] != "tun0" {
				t.Fatalf("set calls = %v ifaces=%v, want 22222 on tun0", qbit.setCalls, qbit.ifaces)
			}
		})
	}
}

func TestRunnerSafeMatchingPrefsNoopDoesNotWrite(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{prefs: QbitPreferences{
		ListenPort:              22222,
		CurrentNetworkInterface: "tun0",
		RandomPort:              false,
		UPnP:                    false,
	}}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if len(qbit.setCalls) != 0 {
		t.Fatalf("set calls = %v, want none", qbit.setCalls)
	}
}

func TestRunnerSuccessfulSyncLogReportsUpdatedCache(t *testing.T) {
	var logs bytes.Buffer
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{listenPort: 11111}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(&logs, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	syncLine := ""
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "action=sync_qbit") {
			syncLine = line
			break
		}
	}
	if syncLine == "" {
		t.Fatalf("sync log not found in logs: %q", logs.String())
	}
	if !strings.Contains(syncLine, "qbit_port=22222") || !strings.Contains(syncLine, "cache_valid=true") {
		t.Fatalf("sync log = %q, want updated qbit/cache fields", syncLine)
	}
	if strings.Contains(syncLine, "qbit_port=11111") {
		t.Fatalf("sync log = %q, contains stale qbit port", syncLine)
	}
}

func TestRunnerFailedQbitWriteDoesNotReturnErrorAndInvalidatesCache(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{listenPort: 11111, setErr: errors.New("set failed")}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if runner.state.CacheValid || runner.state.CachedQbitPort != 11111 {
		t.Fatalf("cache = %+v, want invalid cache retaining last known 11111", runner.state)
	}
}

func TestRunnerQbitWriteFailureForcesNextCycleRevalidation(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222, 22222}}
	qbit := &fakeQbit{listenPort: 11111, setErr: errors.New("set failed")}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	qbit.setErr = nil
	qbit.listenPort = 22222
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 2 {
		t.Fatalf("qbit get calls = %d, want startup read and forced revalidation", qbit.getCalls)
	}
	if len(qbit.setCalls) != 1 {
		t.Fatalf("set calls = %v, want no second write after revalidation", qbit.setCalls)
	}
}

func TestRunnerUnknownCacheFailedQbitReadAndWriteLeavesCacheInvalid(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{22222}}
	qbit := &fakeQbit{getErr: errors.New("get failed"), setErr: errors.New("set failed")}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if runner.state.CacheValid || runner.state.CachedQbitPort != 0 {
		t.Fatalf("cache = %+v, want invalid empty cache", runner.state)
	}
	if qbit.getCalls != 2 {
		t.Fatalf("qbit get calls = %d, want startup read and uncertainty read", qbit.getCalls)
	}
	if len(qbit.setCalls) != 1 || qbit.setCalls[0] != 22222 {
		t.Fatalf("set calls = %v, want one attempted sync to 22222", qbit.setCalls)
	}
}

func TestRunnerLoopContinuesAfterQbitWriteFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gluetun := &fakeGluetun{ports: []int{22222, 22222}}
	qbit := &fakeQbit{listenPort: 11111, setErr: errors.New("set failed")}
	qbit.onSet = func() {
		if len(qbit.setCalls) == 2 {
			cancel()
		}
	}
	cfg := testRunnerConfig()
	cfg.Once = false
	cfg.Interval = time.Nanosecond
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v, want context canceled after continuing", err)
	}
	if len(qbit.setCalls) != 2 {
		t.Fatalf("set calls = %v, want retry on next cycle", qbit.setCalls)
	}
}

func TestRunnerMissingPortBelowThresholdDoesNotReacquire(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{0}}
	qbit := &fakeQbit{listenPort: 11111}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if len(gluetun.statuses) != 0 {
		t.Fatalf("statuses = %v, want none", gluetun.statuses)
	}
	if runner.state.MissingPortFailures != 1 {
		t.Fatalf("missing failures = %d, want 1", runner.state.MissingPortFailures)
	}
}

func TestRunnerLogsDistinctReasonsForMissingAndZeroPort(t *testing.T) {
	var missingLogs bytes.Buffer
	missingGluetun := &fakeGluetun{portStatuses: []PortStatus{{Port: 0, Reason: "missing_port"}}}
	missingRunner := NewRunner(testRunnerConfig(), missingGluetun, &fakeQbit{listenPort: 11111}, NewLogger(&missingLogs, "test"))
	if err := missingRunner.RunOnce(context.Background()); err != nil {
		t.Fatalf("missing RunOnce returned error: %v", err)
	}
	missingLine := actionLine(missingLogs.String(), string(ActionRecordMissingPort))
	if !strings.Contains(missingLine, "reason=missing_port") {
		t.Fatalf("missing port log = %q, want reason=missing_port", missingLine)
	}

	var zeroLogs bytes.Buffer
	zeroGluetun := &fakeGluetun{portStatuses: []PortStatus{{Port: 0, Reason: "zero_port"}}}
	zeroRunner := NewRunner(testRunnerConfig(), zeroGluetun, &fakeQbit{listenPort: 11111}, NewLogger(&zeroLogs, "test"))
	if err := zeroRunner.RunOnce(context.Background()); err != nil {
		t.Fatalf("zero RunOnce returned error: %v", err)
	}
	zeroLine := actionLine(zeroLogs.String(), string(ActionRecordMissingPort))
	if !strings.Contains(zeroLine, "reason=zero_port") {
		t.Fatalf("zero port log = %q, want reason=zero_port", zeroLine)
	}
}

func actionLine(logs, action string) string {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "action="+action) {
			return line
		}
	}
	return ""
}

func TestRunnerMissingPortAtThresholdStopsThenRunsVPN(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{0, 0}}
	qbit := &fakeQbit{listenPort: 11111}
	runner := NewRunner(testRunnerConfig(), gluetun, qbit, NewLogger(ioDiscard{}, "test"))
	runner.reacquireDelay = 0

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if got := strings.Join(gluetun.statuses, ","); got != "stopped,running" {
		t.Fatalf("statuses = %q, want stopped,running", got)
	}
	if runner.state.MissingPortFailures != 0 {
		t.Fatalf("missing failures = %d, want reset to 0", runner.state.MissingPortFailures)
	}
}

func TestRunnerReacquireStartsVPNWhenStopReturnsError(t *testing.T) {
	stopErr := errors.New("stop failed")
	gluetun := &fakeGluetun{
		ports:      []int{0},
		statusErrs: map[string]error{"stopped": stopErr},
	}
	qbit := &fakeQbit{listenPort: 11111}
	cfg := testRunnerConfig()
	cfg.Failures = 1
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))
	runner.reacquireDelay = 0

	err := runner.RunOnce(context.Background())
	if !errors.Is(err, stopErr) {
		t.Fatalf("RunOnce error = %v, want stop error", err)
	}
	if got := strings.Join(gluetun.statuses, ","); got != "stopped,running" {
		t.Fatalf("statuses = %q, want stopped,running", got)
	}
}

func TestRunnerReacquireForcesNextCycleQbitRevalidation(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{0, 12345}}
	qbit := &fakeQbit{listenPort: 12345}
	cfg := testRunnerConfig()
	cfg.Failures = 1
	cfg.QbitAuditInterval = time.Hour
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))
	runner.reacquireDelay = 0

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 2 {
		t.Fatalf("qbit get calls = %d, want startup read and post-reacquire revalidation", qbit.getCalls)
	}
}

func TestRunnerReacquireUpdatesCooldownState(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{0, 0, 0}}
	qbit := &fakeQbit{listenPort: 11111}
	cfg := testRunnerConfig()
	cfg.Failures = 1
	cfg.Cooldown = time.Hour
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))
	runner.reacquireDelay = 0

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if got := strings.Join(gluetun.statuses, ","); got != "stopped,running" {
		t.Fatalf("statuses = %q, want single reacquire", got)
	}
	if runner.state.LastReacquireAt.IsZero() {
		t.Fatal("LastReacquireAt is zero, want cooldown timestamp")
	}
}

func TestRunnerDryRunDoesNotWriteQbitOrReacquire(t *testing.T) {
	var logs bytes.Buffer
	cfg := testRunnerConfig()
	cfg.DryRun = true
	gluetun := &fakeGluetun{ports: []int{22222, 0, 0}}
	qbit := &fakeQbit{listenPort: 11111}
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(&logs, "test"))
	runner.reacquireDelay = 0

	for i := 0; i < 3; i++ {
		if err := runner.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce %d returned error: %v", i+1, err)
		}
	}
	if len(qbit.setCalls) != 0 {
		t.Fatalf("set calls = %v, want none", qbit.setCalls)
	}
	if len(gluetun.statuses) != 0 {
		t.Fatalf("statuses = %v, want none", gluetun.statuses)
	}
	if !strings.Contains(logs.String(), "dry_run=true") || !strings.Contains(logs.String(), "action=sync_qbit") || !strings.Contains(logs.String(), "action=reacquire_port") {
		t.Fatalf("logs did not include dry-run actions: %q", logs.String())
	}
}

func TestRunnerOnceExitsAfterOneCycle(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{12345, 12345}}
	qbit := &fakeQbit{listenPort: 12345}
	cfg := testRunnerConfig()
	cfg.Once = true
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if gluetun.getPortCalls != 1 {
		t.Fatalf("gluetun get calls = %d, want 1", gluetun.getPortCalls)
	}
}

func TestRunnerAuditRefreshesQbitCache(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{12345, 12345}}
	qbit := &fakeQbit{listenPort: 12345}
	cfg := testRunnerConfig()
	cfg.QbitAuditInterval = time.Nanosecond
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	time.Sleep(time.Millisecond)
	qbit.listenPort = 54321
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 2 {
		t.Fatalf("qbit get calls = %d, want 2", qbit.getCalls)
	}
	if runner.state.CachedQbitPort != 12345 {
		t.Fatalf("cache = %+v, want resynced to gluetun 12345", runner.state)
	}
}

func TestRunnerAuditsAfterCacheCreatedByWriteWithoutRead(t *testing.T) {
	gluetun := &fakeGluetun{ports: []int{12345, 12345}}
	qbit := &fakeQbit{listenPort: 11111, getErr: errors.New("read failed")}
	cfg := testRunnerConfig()
	cfg.QbitAuditInterval = time.Nanosecond
	runner := NewRunner(cfg, gluetun, qbit, NewLogger(ioDiscard{}, "test"))

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce returned error: %v", err)
	}
	if len(qbit.setCalls) != 1 || qbit.setCalls[0] != 12345 {
		t.Fatalf("set calls = %v, want sync to 12345", qbit.setCalls)
	}
	qbit.getErr = nil
	time.Sleep(time.Millisecond)
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce returned error: %v", err)
	}
	if qbit.getCalls != 3 {
		t.Fatalf("qbit get calls = %d, want startup read, uncertainty read, and later audit", qbit.getCalls)
	}
}

func TestLoggerWritesKeyValueLines(t *testing.T) {
	var out bytes.Buffer
	logger := NewLogger(&out, "test-name")

	logger.Log("sync_qbit", map[string]any{"port": 12345, "dry_run": true})

	line := out.String()
	for _, want := range []string{"name=test-name", "action=sync_qbit", "port=12345", "dry_run=true", "\n"} {
		if !strings.Contains(line, want) {
			t.Fatalf("log line %q missing %q", line, want)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
