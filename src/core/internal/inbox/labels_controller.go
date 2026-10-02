package inbox

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type labelRequest struct {
	LabelID string `json:"label_id" binding:"required"`
}

type labelResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type labelsResponse struct {
	ID     string          `json:"id"`
	Labels []labelResponse `json:"labels"`
}

// presentLabels answers an empty list and never null, so the frontend draws a row with no labels without a special case.
func presentLabels(tags []Label) []labelResponse {
	out := make([]labelResponse, 0, len(tags))
	for _, l := range tags {
		out = append(out, labelResponse{ID: l.ID.String(), Name: l.Name, Color: l.Color})
	}
	return out
}

// assignLabel answers the labels the conversation ends with, which is also what tagging it twice gets back (ADR-0057).
func (c *Controller) assignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}

	var req labelRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a label_id is required").WithCause(err))
		return
	}
	labelID, ok := c.labelIDFrom(gc, req.LabelID)
	if !ok {
		return
	}

	tags, err := c.service.AssignLabel(gc.Request.Context(), scope, conversationID, labelID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, labelsResponse{ID: conversationID.String(), Labels: presentLabels(tags)})
}

// unassignLabel takes the label from the path, because a DELETE names what it removes by its address.
func (c *Controller) unassignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}
	labelID, ok := c.labelIDFrom(gc, gc.Param("label_id"))
	if !ok {
		return
	}

	tags, err := c.service.UnassignLabel(gc.Request.Context(), scope, conversationID, labelID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, labelsResponse{ID: conversationID.String(), Labels: presentLabels(tags)})
}

func (c *Controller) labelIDFrom(gc *gin.Context, raw string) (uuid.UUID, bool) {
	labelID, err := uuid.Parse(raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_label_id", "the label id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return labelID, true
}
