package contacts

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type assignLabelRequest struct {
	LabelID string `json:"label_id" binding:"required"`
}

// assignLabel answers the whole card and not just the tag, because the caller drew a card and what changed is on it.
// Tagging twice is not refused: the request says what the contact should carry, and after the second one it carries it.
func (c *Controller) assignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var req assignLabelRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a label_id is required").WithCause(err))
		return
	}
	labelID, err := uuid.Parse(req.LabelID)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the label id is not a uuid").WithCause(err))
		return
	}

	if err := c.service.AssignLabel(gc.Request.Context(), scope, contactID, labelID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	c.respondWithCard(gc, scope, contactID)
}

// unassignLabel takes the label from the path and not from a body, because a DELETE identifies what it removes by its address.
func (c *Controller) unassignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	labelID, err := uuid.Parse(gc.Param("label_id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the label id is not a uuid").WithCause(err))
		return
	}

	if err := c.service.UnassignLabel(gc.Request.Context(), scope, contactID, labelID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	c.respondWithCard(gc, scope, contactID)
}
