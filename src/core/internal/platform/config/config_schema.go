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

	// Signs the session token of the operator API and bounds how long one is accepted (ADR-0030).
	JWTSecret string
	JWTTTL    time.Duration

	// The raw 32 bytes, already decoded: whoever holds the Config must not have to learn that the environment carries them in base64 (ADR-0031).
	EncryptionKey []byte

	// Read but never required: only `make seed-admin` needs these, so a missing one must not stop the process from serving (regla 3).
	SeedAdminEmail       string
	SeedAdminPassword    string
	SeedAdminAccountName string

	// Read but never required, like the seed: only `make connect-inbox` needs them, and the serving process never reads them (ADR-0031).
	ConnectInboxPhoneNumberID string
	ConnectInboxAccessToken   string
	ConnectInboxAppSecret     string

	BrainBaseURL       string
	BrainInternalToken string
	BrainTimeout       time.Duration
	PreviewTimeout     time.Duration

	TurnMaxRetries   int
	TurnRetryBackoff time.Duration
	TurnBudget       time.Duration

	// The access token is not here: it belongs to the inbox row, because one account can connect several numbers (F29).
	MetaGraphBaseURL  string
	MetaAPIVersion    string
	WhatsAppTimeout   time.Duration
	WhatsAppUseDouble bool
}
