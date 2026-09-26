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


# One datum of the contact the brain found, in the clear: the core encrypts it and decides whether it is kept (ADR-0047, 2.1).
class ExtractedDatum(BaseModel):
    key: Literal["dni", "cuit_cuil", "cbu", "first_name", "last_name", "email"]
    value: str
    # A number and not a string: it is a confidence between 0 and 1, not money, so C08 does not reach it.
    confidence: float
    source_message_id: UUID


# blocked needs the guardrails of Fase 8 and transfer the classifiers of Fase 9; both are declared because the contract is section 1 whole.
class TurnResponse(BaseModel):
    outcome: Literal["generated", "blocked", "failed"]
    generation_id: UUID | None = None
    completion_text: str | None = None
    transfer: TransferProposal | None = None
    # The fixed answer of decision O2, which only a blocked turn carries: the text the core sends so a refused turn does not leave
    # the customer talking to nobody. A field of its own and not completion_text, because that one is a generation and section 4.2
    # says a refused generation must not be sendable. Null is silence, which is what decision O1c asks for outside the domain.
    notice_text: str | None = None
    # Empty when the toggle of the agent is off, or nothing was found (ADR-0047, sections 2.4 and 2.7).
    extracted: list[ExtractedDatum] = []
