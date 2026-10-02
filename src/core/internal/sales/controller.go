package sales

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type Controller struct {
	service *Service
	log     *slog.Logger
}

func NewController(service *Service, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{service: service, log: log}
}

type saleResponse struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	OperatorID     string  `json:"operator_id"`
	AIAgentID      *string `json:"ai_agent_id"`
	// A string, so the frontend never rounds the amount through a JavaScript number (C08).
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	SourceChannel string `json:"source_channel"`
	CreatedAt     string `json:"created_at"`
}

type listResponse struct {
	Items []saleResponse `json:"items"`
}

func present(s Sale) saleResponse {
	out := saleResponse{
		ID: s.ID.String(), ConversationID: s.ConversationID.String(), OperatorID: s.OperatorID.String(),
		Amount: s.Amount, Currency: s.Currency, SourceChannel: string(s.SourceChannel), CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339),
	}
	if s.AIAgentID != nil {
		agent := s.AIAgentID.String()
		out.AIAgentID = &agent
	}
	return out
}

// The amount is a JSON string on purpose: a JSON number would be decoded through a float before anybody could refuse it (C08).
type markRequest struct {
	Amount   string `json:"amount" binding:"required"`
	Currency string `json:"currency"`
}

func (c *Controller) mark(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.idFrom(gc, "invalid_conversation_id")
	if !ok {
		return
	}
	var req markRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "amount is required, as a decimal in a string").WithCause(err))
		return
	}
	sale, err := c.service.Mark(gc.Request.Context(), scope, conversationID, req.Amount, req.Currency)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(sale))
}

func (c *Controller) ofConversation(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.idFrom(gc, "invalid_conversation_id")
	if !ok {
		return
	}
	sales, err := c.service.OfConversation(gc.Request.Context(), scope, conversationID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	out := listResponse{Items: make([]saleResponse, 0, len(sales))}
	for _, s := range sales {
		out.Items = append(out.Items, present(s))
	}
	soniahttp.OK(gc, out)
}

// Pointers so a field left out is told apart from one sent empty: a correction sends only what changes.
type updateRequest struct {
	Amount   *string `json:"amount"`
	Currency *string `json:"currency"`
}

func (c *Controller) update(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	saleID, ok := c.idFrom(gc, "invalid_sale_id")
	if !ok {
		return
	}
	var req updateRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "amount and currency are the fields of a sale").WithCause(err))
		return
	}
	sale, err := c.service.Update(gc.Request.Context(), scope, saleID, req.Amount, req.Currency)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(sale))
}

func (c *Controller) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	saleID, ok := c.idFrom(gc, "invalid_sale_id")
	if !ok {
		return
	}
	if err := c.service.Delete(gc.Request.Context(), scope, saleID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *Controller) idFrom(gc *gin.Context, code string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest(code, "the id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return parsed, true
}

func (c *Controller) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role}, true
}

func translate(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("sale_not_found", "no such sale").WithCause(err)
	case errors.Is(err, ErrConversationNotFound):
		return soniahttp.NotFound("conversation_not_found", "no such conversation").WithCause(err)
	case errors.Is(err, ErrNotAssigned):
		return soniahttp.Conflict("conversation_not_assigned", "the conversation has no vendor to credit the sale to: take it first").WithCause(err)
	case errors.Is(err, ErrNotTheVendor):
		return soniahttp.Forbidden("not_the_vendor", "only the vendor credited, a supervisor or an admin changes a sale").WithCause(err)
	case errors.Is(err, ErrInvalidAmount):
		return soniahttp.BadRequest("invalid_amount", "the amount is a positive decimal with at most two places, in a string").WithCause(err)
	case errors.Is(err, ErrInvalidCurrency):
		return soniahttp.BadRequest("invalid_currency", "the currency is a three-letter ISO 4217 code").WithCause(err)
	default:
		return err
	}
}
