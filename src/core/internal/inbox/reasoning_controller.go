package inbox

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type usedComponentResponse struct {
	Type          string `json:"type"`
	Position      int    `json:"position"`
	VersionNumber int    `json:"version_number"`
	// Absent and not null for everyone but the admin: the key itself would say there is a text this role is not shown (ADR-0049).
	Content *string `json:"content,omitempty"`
}

type reasoningResponse struct {
	GenerationID string                  `json:"generation_id"`
	Status       string                  `json:"status"`
	LLMModel     string                  `json:"llm_model"`
	CreatedAt    string                  `json:"created_at"`
	Components   []usedComponentResponse `json:"components"`
}

// reasoning is F05: which components of the prompt the answer was assembled from (ADR-0049).
func (c *Controller) reasoning(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}
	generationID, err := uuid.Parse(gc.Param("generation_id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_generation_id", "the generation id is not a uuid").WithCause(err))
		return
	}

	r, err := c.service.Reasoning(gc.Request.Context(), scope, conversationID, generationID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	components := make([]usedComponentResponse, 0, len(r.Components))
	for _, u := range r.Components {
		components = append(components, usedComponentResponse{
			Type: string(u.Type), Position: u.Position, VersionNumber: u.VersionNumber, Content: u.Content,
		})
	}
	soniahttp.OK(gc, reasoningResponse{
		GenerationID: r.GenerationID.String(),
		Status:       string(r.Status),
		LLMModel:     r.LLMModel,
		CreatedAt:    r.CreatedAt.UTC().Format(time.RFC3339Nano),
		Components:   components,
	})
}
