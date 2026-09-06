# The contract of the single synchronous hop, transcribed from 07-secuencia-del-turno.md section 1.

from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict

from infra.enums import AgentMode


# Four ids and nothing else: the brain reads the rest from PostgreSQL, so neither service works on its own copy of the turn.
class TurnRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    message_batch_id: UUID
    conversation_id: UUID
    ai_agent_id: UUID
    # Explicit and never deduced, and off is absent because a conversation with the agent off never reaches the brain.
    mode: Literal[AgentMode.AUTONOMOUS, AgentMode.SUGGESTION]


# A proposal and not a fact: transfer_events is written by the core, in the transaction that moves the conversation.
class TransferProposal(BaseModel):
    proposed: bool
    signal_evaluation_id: UUID


# blocked needs the guardrails of Fase 8 and transfer the classifiers of Fase 9; both are declared because the contract is section 1 whole.
class TurnResponse(BaseModel):
    outcome: Literal["generated", "blocked", "failed"]
    generation_id: UUID | None = None
    completion_text: str | None = None
    transfer: TransferProposal | None = None
