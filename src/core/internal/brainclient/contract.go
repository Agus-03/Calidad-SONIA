package brainclient

import (
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// The path of the single synchronous hop (ADR-0017 (e)); it is the brain's route and not a resource of the operator API.
const turnPath = "/turns"

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
}

// Text is the answer to send, empty when the brain generated nothing; the caller decides what an empty answer means.
func (r TurnResponse) Text() string {
	if r.CompletionText == nil {
		return ""
	}
	return *r.CompletionText
}
