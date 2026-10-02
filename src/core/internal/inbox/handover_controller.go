package inbox

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type reassignRequest struct {
	ToOperatorID string `json:"to_operator_id" binding:"required"`
	Reason       string `json:"reason"`
}

type reassignResponse struct {
	ID         string `json:"id"`
	OperatorID string `json:"operator_id"`
}

// reassign answers who holds the conversation now; the row itself reaches the inbox through the hub, like every change (ADR-0051).
func (c *Controller) reassign(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}

	var req reassignRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "to_operator_id is required, and reason says why").WithCause(err))
		return
	}
	to, ok := c.operatorIDFrom(gc, "to_operator_id", req.ToOperatorID)
	if !ok {
		return
	}

	if err := c.service.Reassign(gc.Request.Context(), scope, conversationID, to, req.Reason); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, reassignResponse{ID: conversationID.String(), OperatorID: to.String()})
}

type handoverRequest struct {
	FromOperatorID string `json:"from_operator_id" binding:"required"`
	ToOperatorID   string `json:"to_operator_id" binding:"required"`
	Reason         string `json:"reason"`
}

type handoverResponse struct {
	Reassigned int `json:"reassigned"`
}

// handOver answers how many moved, zero included: a vendor with nothing open is a handover that is already done (ADR-0056).
func (c *Controller) handOver(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}

	var req handoverRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "from_operator_id and to_operator_id are required, and reason says why").WithCause(err))
		return
	}
	from, ok := c.operatorIDFrom(gc, "from_operator_id", req.FromOperatorID)
	if !ok {
		return
	}
	to, ok := c.operatorIDFrom(gc, "to_operator_id", req.ToOperatorID)
	if !ok {
		return
	}

	moved, err := c.service.HandOver(gc.Request.Context(), scope, from, to, req.Reason)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, handoverResponse{Reassigned: moved})
}

type assignmentResponse struct {
	ID string `json:"id"`
	// Null when the conversation came from the pool, and assigned_by null when the dispatcher gave it: nobody did (ADR-0037).
	From       *operatorResponse `json:"from"`
	To         operatorResponse  `json:"to"`
	AssignedBy *operatorResponse `json:"assigned_by"`
	Reason     string            `json:"reason"`
	CreatedAt  string            `json:"created_at"`
}

type assignmentsResponse struct {
	Items []assignmentResponse `json:"items"`
}

// assignments has no cursor: a conversation changes hands a few times, never a page's worth (ADR-0056).
func (c *Controller) assignments(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}

	entries, err := c.service.Assignments(gc.Request.Context(), scope, conversationID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	out := assignmentsResponse{Items: make([]assignmentResponse, 0, len(entries))}
	for _, e := range entries {
		out.Items = append(out.Items, assignmentResponse{
			ID: e.ID.String(), From: person(e.From), To: *person(&e.To), AssignedBy: person(e.AssignedBy),
			Reason: e.Reason, CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	soniahttp.OK(gc, out)
}

func person(o *Operator) *operatorResponse {
	if o == nil {
		return nil
	}
	return &operatorResponse{ID: o.ID.String(), Name: o.Name, Lastname: o.Lastname}
}

func (c *Controller) conversationIDFrom(gc *gin.Context) (uuid.UUID, bool) {
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return conversationID, true
}

// operatorIDFrom names the field in the code, so the frontend knows which of the two ids of a handover was wrong.
func (c *Controller) operatorIDFrom(gc *gin.Context, field, raw string) (uuid.UUID, bool) {
	operatorID, err := uuid.Parse(raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_"+field, field+" is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return operatorID, true
}
