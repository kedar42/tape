package portwatch

import (
	"context"
	"time"
)

type Config struct {
	GluetunURL        string
	QbitURL           string
	APIKeyFile        string
	QbitAPIKeyFile    string
	QbitUsername      string
	QbitPasswordFile  string
	Interval          time.Duration
	Failures          int
	Cooldown          time.Duration
	HTTPTimeout       time.Duration
	QbitAuditInterval time.Duration
	RecoveryInterval  time.Duration
	RecoveryDuration  time.Duration
	QbitInterface     string
	Once              bool
	DryRun            bool
}

type WatchState struct {
	CachedQbitPort      int
	CacheValid          bool
	ForceQbitSync       bool
	MissingPortFailures int
	LastReacquireAt     time.Time
}

type PortStatus struct {
	Port   int
	Reason string
}

type QbitPreferences struct {
	ListenPort              int
	CurrentNetworkInterface string
	RandomPort              bool
	UPnP                    bool
}

type QbitAuth struct {
	APIKey   string
	Username string
	Password string
}

type GluetunAPI interface {
	GetForwardedPort(context.Context) (int, error)
	SetVPNStatus(context.Context, string) error
}

type GluetunPortStatusAPI interface {
	GetForwardedPortStatus(context.Context) (PortStatus, error)
}

type QbitAPI interface {
	GetListenPort(context.Context) (int, error)
	SetListenPort(context.Context, int, string) error
}

type QbitPreferencesAPI interface {
	GetPreferences(context.Context) (QbitPreferences, error)
}
