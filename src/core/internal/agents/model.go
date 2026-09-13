// Package agents owns the identity of the AI agent: ai_agents, its prompt components and their versions, and the audit of every
// change to them (F01, F17, ADR-0007). The bot is an AIAgent and never an Agent alone (C01).
package agents

import (
	"time"

	"github.com/google/uuid"
)

// Agent is the live row. default_mode and extraction_enabled are deliberately not here: neither changes anything until Fase 9,
// and a field that saves without effect tells the administrator something that is not true (ADR-0038).
type Agent struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Name        string
	Description *string
	LLMProvider string
	LLMModel    string
	// A float and not a decimal: C08 is about money, and the brain hands the provider a float anyway.
	Temperature *float64
	MaxTokens   *int
	// A pause that keeps the configuration: removing the agent is deleted_at, which no read of this package returns (ADR-0038).
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Config is what an administrator edits, and the shape the audit compares: the timestamps move on every write and would make every
// before differ from its after.
type Config struct {
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	LLMProvider string   `json:"llm_provider"`
	LLMModel    string   `json:"llm_model"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
	Active      bool     `json:"active"`
}

func (a Agent) Config() Config {
	return Config{
		Name: a.Name, Description: a.Description, LLMProvider: a.LLMProvider, LLMModel: a.LLMModel,
		Temperature: a.Temperature, MaxTokens: a.MaxTokens, Active: a.Active,
	}
}

// equal compares what the pointers hold, which is what a save with no change has to recognise so it writes nothing.
func (c Config) equal(o Config) bool {
	return c.Name == o.Name && c.LLMProvider == o.LLMProvider && c.LLMModel == o.LLMModel && c.Active == o.Active &&
		samePtr(c.Description, o.Description) && samePtr(c.Temperature, o.Temperature) && samePtr(c.MaxTokens, o.MaxTokens)
}

func samePtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// The three verbs of the audit and the entity they name: stable words and not sentences, so a query can filter on them (ADR-0007).
const (
	actionCreate = "create"
	actionUpdate = "update"
	actionDelete = "delete"

	entityAIAgent = "ai_agents"
)

// AuditEntry is one row of audit_logs. ip_address is left NULL on purpose: behind no trusted proxy the client address is whatever
// the caller writes in X-Forwarded-For, and a forged address is worse evidence than none (ADR-0038).
type AuditEntry struct {
	ID         uuid.UUID
	OperatorID uuid.UUID
	Action     string
	EntityType string
	EntityID   uuid.UUID
	Changes    []byte
}

// change is what changes_json holds: the configuration before and after, NULL on the side that did not exist.
type change struct {
	Before *Config `json:"before"`
	After  *Config `json:"after"`
	// Only on a removal: nothing else records which inboxes lost their default when the agent went away.
	ClearedInboxIDs []uuid.UUID `json:"cleared_inbox_ids,omitempty"`
}
