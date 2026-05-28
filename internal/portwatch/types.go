package portwatch

import (
	"context"
	"time"
)

type Config struct {
	Name              string
	GluetunURL        string
	QbitURL           string
	APIKeyFile        string
	Interval          time.Duration
	Failures          int
	Cooldown          time.Duration
	HTTPTimeout       time.Duration
	QbitAuditInterval time.Duration
	QbitInterface     string
	Once              bool
	DryRun            bool
}

type WatchState struct {
	CachedQbitPort      int
	CacheValid          bool
	MissingPortFailures int
	LastReacquireAt     time.Time
}

type PortStatus struct {
	Port   int
	Reason string
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
