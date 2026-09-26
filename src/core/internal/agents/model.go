// Package agents owns the identity of the AI agent: ai_agents, its prompt components and their versions, and the audit of every
// change to them (F01, F17, ADR-0007). The bot is an AIAgent and never an Agent alone (C01).
package agents

import (
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// AIAgent is the live row. default_mode is deliberately not here: it changes nothing yet, and a field that saves without effect
// tells the administrator something that is not true (ADR-0038). extraction_enabled is, since F31 reads it (ADR-0047, 2.4).
type AIAgent struct {
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
	Active bool
	// The toggle of F31: off, the brain extracts nothing and never calls the extractor (ADR-0047, section 2.4).
	ExtractionEnabled bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
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
	// Audited like the rest: switching what is kept about the contacts is a change of configuration (ADR-0038, regla 7).
	ExtractionEnabled bool `json:"extraction_enabled"`
}

func (a AIAgent) Config() Config {
	return Config{
		Name: a.Name, Description: a.Description, LLMProvider: a.LLMProvider, LLMModel: a.LLMModel,
		Temperature: a.Temperature, MaxTokens: a.MaxTokens, Active: a.Active, ExtractionEnabled: a.ExtractionEnabled,
	}
}

// equal compares what the pointers hold, which is what a save with no change has to recognise so it writes nothing.
func (c Config) equal(o Config) bool {
	return c.Name == o.Name && c.LLMProvider == o.LLMProvider && c.LLMModel == o.LLMModel && c.Active == o.Active &&
		c.ExtractionEnabled == o.ExtractionEnabled &&
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
	// Which agent answers a number is agent configuration too: it decides against which configuration a conversation is measured (OE2).
	entityInbox = "inboxes"
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

// inboxDefault is the one field of an inbox this package audits, in the same before and after shape as an agent's change.
type inboxDefault struct {
	DefaultAIAgentID *uuid.UUID `json:"default_ai_agent_id"`
}

type inboxChange struct {
	Before inboxDefault `json:"before"`
	After  inboxDefault `json:"after"`
}

// change is what changes_json holds: the configuration before and after, NULL on the side that did not exist.
type change struct {
	Before *Config `json:"before"`
	After  *Config `json:"after"`
	// Only on a removal: nothing else records which inboxes lost their default when the agent went away.
	ClearedInboxIDs []uuid.UUID `json:"cleared_inbox_ids,omitempty"`
	// Only on a pause or a removal that turned some bot off: resuming the agent turns none back on, so this is the one record of
	// which conversations it left to their vendor (ADR-0038).
	TurnedOffConversationIDs []uuid.UUID `json:"turned_off_conversation_ids,omitempty"`
}

// Component is one of the six blocks of F01 as it stands now. There is one per type and agent: editing it is writing a new text for
// the same block, which is why every change lands as a version and never as a second row (ADR-0039).
type Component struct {
	ID        uuid.UUID
	AIAgentID uuid.UUID
	Type      enums.PromptComponentType
	Content   string
	// Where the block goes in the prompt: sent by the panel, which knows the order prompting asks for, and fixed from then on (ADR-0039).
	Position int16
	// Off is ablation: the block exists and is not assembled. Removing it is deleted_at, which no read of this package returns.
	Active bool
	// The number of the version the row is at, the one a generation assembled from it names (H-20).
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewComponent is what the panel sends to add a block. A component is born active, the same as an agent.
type NewComponent struct {
	Type    enums.PromptComponentType
	Content string
	// An int and not the column's smallint, so a number too large is refused as a position and not as a body of the wrong type.
	Position   int
	ChangeNote *string
}

// ComponentEdit is what can change of a block after it exists: its text and whether it is assembled. A nil field keeps its value.
type ComponentEdit struct {
	Content    *string
	Active     *bool
	ChangeNote *string
}

// ComponentVersion is one frozen state of a block: what it said, whether it was on, who left it that way and why. It is the audit of
// the components, so audit_logs does not repeat it (ADR-0039).
// VersionPage is one page of a block's history and the size of all of it.
type VersionPage struct {
	Items []ComponentVersion
	Total int
}

type ComponentVersion struct {
	ID          uuid.UUID
	ComponentID uuid.UUID
	Number      int
	Content     string
	Active      bool
	// Nil only for the versions a seed wrote, which nobody edited.
	ChangedBy  *uuid.UUID
	ChangeNote *string
	CreatedAt  time.Time
}
