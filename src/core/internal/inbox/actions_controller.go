package inbox

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type modeRequest struct {
	Mode string `json:"mode" binding:"required"`
}

type modeResponse struct {
	ID   string `json:"id"`
	Mode string `json:"mode"`
}

// setMode answers the mode the conversation ends with, which is also what a request for the mode it already had gets back.
func (c *Controller) setMode(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}

	var req modeRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a mode is required").WithCause(err))
		return
	}

	mode, err := c.service.SetMode(gc.Request.Context(), scope, conversationID, enums.AgentMode(req.Mode))
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, modeResponse{ID: conversationID.String(), Mode: string(mode)})
}

// markRead answers 204: there is nothing to draw back, and the row reaches every inbox by the stream (ADR-0051, ADR-0052).
func (c *Controller) markRead(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}
	if err := c.service.MarkRead(gc.Request.Context(), scope, conversationID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// resolve answers 204 like markRead: the row reaches every inbox by the stream (ADR-0051, ADR-0054).
func (c *Controller) resolve(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}
	if err := c.service.Resolve(gc.Request.Context(), scope, conversationID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// A pointer so false is a value and not an absence: the binding refuses a zero bool as missing (F25).
type starredRequest struct {
	Starred *bool `json:"starred" binding:"required"`
}

type starredResponse struct {
	ID      string `json:"id"`
	Starred bool   `json:"starred"`
}

// setStarred answers the state the conversation ends with, which is also what a request for the state it already had gets back.
func (c *Controller) setStarred(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}

	var req starredRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "starred is required, true or false").WithCause(err))
		return
	}

	starred, err := c.service.SetStarred(gc.Request.Context(), scope, conversationID, *req.Starred)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, starredResponse{ID: conversationID.String(), Starred: starred})
}

// A pointer so a missing score is told apart from a zero, which the service refuses as off the scale anyway (ADR-0055).
type scoreRequest struct {
	Score *int `json:"score" binding:"required"`
}

type scoreResponse struct {
	ID    string `json:"id"`
	Score int    `json:"score"`
}

// setScore answers the score the conversation ends with, which overwrites the one before (ADR-0055).
func (c *Controller) setScore(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}

	var req scoreRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "score is required, a whole number from 1 to 5").WithCause(err))
		return
	}

	score, err := c.service.Score(gc.Request.Context(), scope, conversationID, *req.Score)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, scoreResponse{ID: conversationID.String(), Score: score})
}
