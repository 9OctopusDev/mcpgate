package config

import "time"

func applyDefaults(document *Document) {
	limits := &document.Gateway.Limits
	if limits.MaxInflight == 0 {
		limits.MaxInflight = 256
	}
	if limits.MaxQueued == 0 {
		limits.MaxQueued = 64
	}
	if limits.QueueTimeout.Duration == 0 {
		limits.QueueTimeout.Duration = 250 * time.Millisecond
	}
	if limits.CallTimeout.Duration == 0 {
		limits.CallTimeout.Duration = 30 * time.Second
	}
	if limits.RequestBytes == 0 {
		limits.RequestBytes = 1 << 20
	}
	if limits.ResponseBytes == 0 {
		limits.ResponseBytes = 4 << 20
	}
	if limits.SchemaBytes == 0 {
		limits.SchemaBytes = 2 << 20
	}
	if limits.BufferBudgetBytes == 0 {
		limits.BufferBudgetBytes = 256 << 20
	}
	if limits.PerPrincipalRPS == 0 {
		limits.PerPrincipalRPS = 20
	}
	if limits.PerPrincipalBurst == 0 {
		limits.PerPrincipalBurst = 40
	}

	lifecycle := &document.Gateway.Lifecycle
	if lifecycle.StartupTimeout.Duration == 0 {
		lifecycle.StartupTimeout.Duration = 30 * time.Second
	}
	if lifecycle.DrainTimeout.Duration == 0 {
		lifecycle.DrainTimeout.Duration = 45 * time.Second
	}
	if lifecycle.LegacySessionIdle.Duration == 0 {
		lifecycle.LegacySessionIdle.Duration = 15 * time.Minute
	}
	if lifecycle.MaxLegacySessions == 0 {
		lifecycle.MaxLegacySessions = 1000
	}
}
