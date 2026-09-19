# The contract of F03: the panel sends what is on screen, saved or not, and the brain answers the system the model would read.

from typing import Self
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator

from infra.enums import PromptComponentType

# The ceiling of prompt_components.position, a smallint in 0001: a draft the table could not hold is not previewed either.
_MAX_POSITION = 32767


class DraftComponent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    type: PromptComponentType
    # Blank is allowed: an administrator who empties a block sees its section disappear, the same as it would in a turn.
    content: str
    position: int = Field(ge=1, le=_MAX_POSITION)


# Every block on screen and not only the one being edited, so the brain never merges a draft with what is saved (ADR-0040).
class PreviewRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ai_agent_id: UUID
    components: list[DraftComponent] = Field(max_length=len(PromptComponentType))

    # The two rules the core enforces on a save (ADR-0039), so a preview never shows a prompt that could not be saved.
    @model_validator(mode="after")
    def _one_of_each(self) -> Self:
        types = [c.type for c in self.components]
        if len(set(types)) != len(types):
            raise ValueError("two components of one type: F01 declares one of each")
        positions = [c.position for c in self.components]
        if len(set(positions)) != len(positions):
            raise ValueError("two components at one position: nothing would decide their order")
        return self


# The system alone: the envelope of the customer's message is not previewed (ADR-0040).
class PreviewResponse(BaseModel):
    system: str
    rules_version: int
    # The ones that went in, in order: a blank draft is left out, and the panel can say so.
    components: list[PromptComponentType]
