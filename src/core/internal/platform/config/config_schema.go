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

	// The access token is not here: it belongs to the inbox row, because one account can connect several numbers (F29).
	MetaGraphBaseURL  string
	MetaAPIVersion    string
	WhatsAppTimeout   time.Duration
	WhatsAppUseDouble bool
}
