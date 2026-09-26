package brainclient

import (
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// The path of the single synchronous hop (ADR-0017 (e)); it is the brain's route and not a resource of the operator API.
const turnPath = "/turns"

// What a turn can run on, which the core checks before saving an agent (ADR-0038).
const modelsPath = "/models"

// The preview of F03, assembled by the brain because the assembler has one implementation (ADR-0017, ADR-0040).
const previewPath = "/prompts/preview"

// The outcome is a wire value of this hop and not a column, so it is not one of the 22 enumerations of contracts/enums.yaml.
const (
	OutcomeGenerated = "generated"
	OutcomeBlocked   = "blocked"
	OutcomeFailed    = "failed"
)

// TurnRequest mirrors api/schemas/turn.py: four ids and the mode, because the brain reads the rest from PostgreSQL itself.
type TurnRequest struct {
	MessageBatchID uuid.UUID       `json:"message_batch_id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	AIAgentID      uuid.UUID       `json:"ai_agent_id"`
	Mode           enums.AgentMode `json:"mode"`
}

// TransferProposal is a proposal and not a fact: transfer_events is written here, in the transaction that moves the conversation.
type TransferProposal struct {
	Proposed           bool      `json:"proposed"`
	SignalEvaluationID uuid.UUID `json:"signal_evaluation_id"`
}

// Pointers where the contract says null: an absent generation_id is evidence nobody wrote, which is not the zero uuid.
type TurnResponse struct {
	Outcome        string            `json:"outcome"`
	GenerationID   *uuid.UUID        `json:"generation_id"`
	CompletionText *string           `json:"completion_text"`
	Transfer       *TransferProposal `json:"transfer"`
	// The fixed text of a cut turn (decision O2), which is not a generation and travels in its own field for that reason: a
	// refused answer must not be sendable (07-secuencia 4.2) and this one is the only thing the customer may read.
	NoticeText *string `json:"notice_text"`
	// What the brain found about the contact, in the clear: the core encrypts it and decides what is kept (ADR-0047, 2.1).
	Extracted []ExtractedDatum `json:"extracted"`
}

// ExtractedDatum mirrors api/schemas/turn.py. The key is one of the six of ADR-0047, and contacts/ refuses any other.
type ExtractedDatum struct {
	Key             string    `json:"key"`
	Value           string    `json:"value"`
	Confidence      float64   `json:"confidence"`
	SourceMessageID uuid.UUID `json:"source_message_id"`
}

// PricedModel mirrors api/schemas/models.py: a provider and a model the tariff file of the brain can price.
type PricedModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ModelsResponse carries the tariff version so a refusal can name the file that decided it (regla 9).
type ModelsResponse struct {
	PricingVersion int           `json:"pricing_version"`
	Models         []PricedModel `json:"models"`
}

// DraftComponent mirrors api/schemas/preview.py: a block as it is on screen, saved or not, which is why it carries no id.
type DraftComponent struct {
	Type     enums.PromptComponentType `json:"type"`
	Content  string                    `json:"content"`
	Position int                       `json:"position"`
}

// PreviewRequest carries every block on screen, so the brain never merges a draft with what is saved (ADR-0040).
type PreviewRequest struct {
	AIAgentID  uuid.UUID        `json:"ai_agent_id"`
	Components []DraftComponent `json:"components"`
}

// PreviewResponse is the system alone, with the types that went in, in the order they went in (ADR-0040).
type PreviewResponse struct {
	System       string                      `json:"system"`
	RulesVersion int                         `json:"rules_version"`
	Components   []enums.PromptComponentType `json:"components"`
}

// Text is the answer to send, empty when the brain generated nothing; the caller decides what an empty answer means.
func (r TurnResponse) Text() string {
	if r.CompletionText == nil {
		return ""
	}
	return *r.CompletionText
}

// Notice is the fixed text of a cut turn, empty when the pipeline decided the customer is told nothing at all (decision O1c).
func (r TurnResponse) Notice() string {
	if r.NoticeText == nil {
		return ""
	}
	return *r.NoticeText
}
