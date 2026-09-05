package config

import "time"

// Config is the validated configuration of the core deployable.
type Config struct {
	HTTPAddr    string
	Environment string
	LogLevel    string

	DatabaseURL   string
	DBPoolMinSize int
	DBPoolMaxSize int

	BrainBaseURL       string
	BrainInternalToken string
	BrainTimeout       time.Duration
	PreviewTimeout     time.Duration

	TurnMaxRetries   int
	TurnRetryBackoff time.Duration
}
