package reminders

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

type reminderResponse struct {
	ID             string  `json:"id"`
	ContactID      string  `json:"contact_id"`
	ConversationID *string `json:"conversation_id"`
	// Who created it; who is told is decided when it falls due, and the alert is how that person learns it (ADR-0058).
	OperatorID  string  `json:"operator_id"`
	Description string  `json:"description"`
	DueAt       string  `json:"due_at"`
	Notified    bool    `json:"notified"`
	NotifiedAt  *string `json:"notified_at"`
	CreatedAt   string  `json:"created_at"`
}

type listResponse struct {
	Items []reminderResponse `json:"items"`
}

func present(r Reminder) reminderResponse {
	out := reminderResponse{
		ID: r.ID.String(), ContactID: r.ContactID.String(), OperatorID: r.OperatorID.String(), Description: r.Description,
		DueAt: r.DueAt.UTC().Format(time.RFC3339), Notified: r.Notified, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
	if r.ConversationID != nil {
		conversation := r.ConversationID.String()
		out.ConversationID = &conversation
	}
	if r.NotifiedAt != nil {
		at := r.NotifiedAt.UTC().Format(time.RFC3339)
		out.NotifiedAt = &at
	}
	return out
}

func presentAll(rs []Reminder) listResponse {
	out := listResponse{Items: make([]reminderResponse, 0, len(rs))}
	for _, r := range rs {
		out.Items = append(out.Items, present(r))
	}
	return out
}

type createRequest struct {
	Description    string  `json:"description"`
	DueAt          string  `json:"due_at" binding:"required"`
	ConversationID *string `json:"conversation_id"`
}

func (c *Controller) create(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, ok := c.idFrom(gc, "id", "invalid_contact_id")
	if !ok {
		return
	}
	var req createRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "due_at is required, and description says what to do").WithCause(err))
		return
	}
	dueAt, ok := c.instant(gc, req.DueAt)
	if !ok {
		return
	}
	var conversationID *uuid.UUID
	if req.ConversationID != nil {
		parsed, err := uuid.Parse(*req.ConversationID)
		if err != nil {
			soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
			return
		}
		conversationID = &parsed
	}

	r, err := c.service.Create(gc.Request.Context(), scope, contactID, conversationID, req.Description, dueAt)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(r))
}

func (c *Controller) ofContact(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, ok := c.idFrom(gc, "id", "invalid_contact_id")
	if !ok {
		return
	}
	rs, err := c.service.OfContact(gc.Request.Context(), scope, contactID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, presentAll(rs))
}

// mine turns on only with due=true, the same rule as the flags of the inbox: anything else is refused, not guessed (ADR-0050, regla 10).
func (c *Controller) mine(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	raw, asked := gc.GetQuery("due")
	if asked && raw != "true" {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_filter", "due is turned on with true, and left out otherwise"))
		return
	}
	rs, err := c.service.Mine(gc.Request.Context(), scope, asked)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, presentAll(rs))
}

// Pointers so a field left out is told apart from one sent empty: an edit sends only what changes.
type updateRequest struct {
	Description *string `json:"description"`
	DueAt       *string `json:"due_at"`
}

func (c *Controller) update(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	reminderID, ok := c.idFrom(gc, "id", "invalid_reminder_id")
	if !ok {
		return
	}
	var req updateRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "description and due_at are the fields of a reminder").WithCause(err))
		return
	}
	var dueAt *time.Time
	if req.DueAt != nil {
		parsed, ok := c.instant(gc, *req.DueAt)
		if !ok {
			return
		}
		dueAt = &parsed
	}

	r, err := c.service.Update(gc.Request.Context(), scope, reminderID, req.Description, dueAt)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(r))
}

func (c *Controller) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	reminderID, ok := c.idFrom(gc, "id", "invalid_reminder_id")
	if !ok {
		return
	}
	if err := c.service.Delete(gc.Request.Context(), scope, reminderID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *Controller) idFrom(gc *gin.Context, param, code string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(gc.Param(param))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest(code, "the id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return parsed, true
}

// An instant with its offset, as every instant of the API travels: a date without a zone is three hours off in Córdoba (C06).
func (c *Controller) instant(gc *gin.Context, raw string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_due_at", "due_at is an RFC 3339 instant with its offset").WithCause(err))
		return time.Time{}, false
	}
	return at, true
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
		return soniahttp.NotFound("reminder_not_found", "no such reminder").WithCause(err)
	case errors.Is(err, ErrContactNotFound):
		return soniahttp.NotFound("contact_not_found", "no such contact").WithCause(err)
	case errors.Is(err, ErrConversationNotOfContact):
		return soniahttp.BadRequest("conversation_not_of_contact", "the conversation is not one of the contact").WithCause(err)
	case errors.Is(err, ErrEmptyDescription):
		return soniahttp.BadRequest("invalid_description", "the reminder needs a description").WithCause(err)
	case errors.Is(err, ErrDueInThePast):
		return soniahttp.BadRequest("due_at_in_the_past", "a reminder is due in the future").WithCause(err)
	case errors.Is(err, ErrNotTheOwner):
		return soniahttp.Forbidden("not_the_owner", "only its owner, a supervisor or an admin changes a reminder").WithCause(err)
	default:
		return err
	}
}
