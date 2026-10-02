package inbox

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// The same page size and ceiling as contacts/, so the two listings differ in how they continue and in nothing else (ADR-0036, regla 8).
	defaultLimit = 20
	maxLimit     = 100
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

type pageResponse struct {
	Items []summaryResponse `json:"items"`
	// Null at the end, and never an empty string: a frontend that sends "" back would be asking for the first page again (ADR-0036, regla 6).
	NextCursor *string `json:"next_cursor"`
}

type summaryResponse struct {
	ID        string  `json:"id"`
	Status    string  `json:"status"`
	Mode      string  `json:"mode"`
	AIAgentID *string `json:"ai_agent_id"`
	// False when there is no agent or it is paused or removed: the panel greys the switch out instead of trying it (ADR-0038).
	AIAgentAvailable bool              `json:"ai_agent_available"`
	Starred          bool              `json:"starred"`
	UnreadCount      int               `json:"unread_count"`
	QualityScore     *int              `json:"quality_score"`
	Labels           []labelResponse   `json:"labels"`
	Contact          contactResponse   `json:"contact"`
	Operator         *operatorResponse `json:"operator"`
	LastMessage      *messageResponse  `json:"last_message"`
	FailedCount      int               `json:"failed_count"`
	LastFailedAt     *string           `json:"last_failed_at"`
	LastInboundAt    string            `json:"last_inbound_at"`
	WindowExpiresAt  string            `json:"window_expires_at"`
	LastActivityAt   string            `json:"last_activity_at"`
}

type contactResponse struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Lastname      string `json:"lastname"`
	PhoneWhatsapp string `json:"phone_whatsapp"`
}

type operatorResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Lastname string `json:"lastname"`
}

type messageResponse struct {
	ID          string  `json:"id"`
	Direction   string  `json:"direction"`
	SenderType  string  `json:"sender_type"`
	ContentText string  `json:"content_text"`
	ContentType string  `json:"content_type"`
	Status      *string `json:"status"`
	ErrorCode   *string `json:"error_code"`
	SentAt      string  `json:"sent_at"`
}

// EventRow is the row of the listing as realtime/ sends it, so an event and a page never disagree on its shape (ADR-0051, ADR-0063).
func EventRow(s Summary) any { return present(s) }

func present(s Summary) summaryResponse {
	out := summaryResponse{
		ID:     s.ID.String(),
		Status: string(s.Status),
		Mode:   string(s.Mode),
		Contact: contactResponse{
			ID: s.Contact.ID.String(), Name: s.Contact.Name, Lastname: s.Contact.Lastname, PhoneWhatsapp: s.Contact.PhoneWhatsapp,
		},
		AIAgentAvailable: s.AIAgentAvailable,
		Starred:          s.Starred,
		UnreadCount:      s.UnreadCount,
		QualityScore:     s.QualityScore,
		Labels:           presentLabels(s.Labels),
		FailedCount:      s.FailedCount,
		LastFailedAt:     rfc3339(s.LastFailedAt),
		LastInboundAt:    s.LastInboundAt.UTC().Format(time.RFC3339),
		WindowExpiresAt:  s.WindowExpiresAt.UTC().Format(time.RFC3339),
		LastActivityAt:   s.LastActivityAt.UTC().Format(time.RFC3339),
	}
	if s.AIAgentID != nil {
		agent := s.AIAgentID.String()
		out.AIAgentID = &agent
	}
	if s.Operator != nil {
		out.Operator = &operatorResponse{ID: s.Operator.ID.String(), Name: s.Operator.Name, Lastname: s.Operator.Lastname}
	}
	if s.LastMessage != nil {
		drawn := presentMessage(*s.LastMessage)
		out.LastMessage = &drawn
	}
	return out
}

// presentMessage is the one place a message becomes JSON, so the listing and the answer just sent can never disagree on its shape.
func presentMessage(m Message) messageResponse {
	out := messageResponse{
		ID: m.ID.String(), Direction: string(m.Direction), SenderType: string(m.SenderType),
		ContentText: m.ContentText, ContentType: string(m.ContentType),
		ErrorCode: m.ErrorCode, SentAt: m.SentAt.UTC().Format(time.RFC3339),
	}
	if m.Status != nil {
		status := string(*m.Status)
		out.Status = &status
	}
	return out
}

func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func (c *Controller) list(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}

	limit, err := limitFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	after, err := cursorFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	filter, err := filterFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	page, err := c.service.List(gc.Request.Context(), scope, filter, after, limit)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	items := make([]summaryResponse, 0, len(page.Items))
	for _, one := range page.Items {
		items = append(items, present(one))
	}
	body := pageResponse{Items: items}
	if page.Next != nil {
		next := encodeCursor(*page.Next)
		body.NextCursor = &next
	}
	soniahttp.OK(gc, body)
}

// threadMessageResponse is the message every endpoint draws plus who wrote it, embedded so the base shape stays the one presentMessage
// builds: the thread adds a field, it never redefines one.
type threadMessageResponse struct {
	messageResponse
	SenderOperator *operatorResponse `json:"sender_operator"`
	// What the frontend asks F05 about; null for what nobody generated (ADR-0049).
	GenerationID *string `json:"generation_id"`
}

type threadPageResponse struct {
	Items      []threadMessageResponse `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

// thread takes the same limit and the same opaque cursor as the listing, so the two cannot disagree on how a page continues (ADR-0036, regla 1).
func (c *Controller) thread(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}
	limit, err := limitFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	after, err := cursorFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	page, err := c.service.Thread(gc.Request.Context(), scope, conversationID, after, limit)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	items := make([]threadMessageResponse, 0, len(page.Items))
	for _, m := range page.Items {
		drawn := threadMessageResponse{messageResponse: presentMessage(m.Message)}
		if m.SenderOperator != nil {
			drawn.SenderOperator = &operatorResponse{ID: m.SenderOperator.ID.String(), Name: m.SenderOperator.Name, Lastname: m.SenderOperator.Lastname}
		}
		if m.GenerationID != nil {
			generation := m.GenerationID.String()
			drawn.GenerationID = &generation
		}
		items = append(items, drawn)
	}
	body := threadPageResponse{Items: items}
	if page.Next != nil {
		next := encodeCursor(*page.Next)
		body.NextCursor = &next
	}
	soniahttp.OK(gc, body)
}

// Not required by the binding: an absent text and a blank one are the same refusal, and the service is the one that answers it, with
// one code (ADR-0035, regla 9). The binding still refuses a body that is not json at all.
type sendRequest struct {
	Text string `json:"text"`
	// The suggestion the text was copied from, when it was: absent is an answer the Operator wrote alone (ADR-0046, section 2.5).
	GenerationID *string `json:"generation_id"`
}

// send answers 201 with the row whether Meta took it or not: the row exists either way, and its status is what the operator has to
// read. A failed send is ADR-0020 working as decided, not an error of the request.
func (c *Controller) send(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}

	var req sendRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "the body is not a json object with a text").WithCause(err))
		return
	}

	var generationID *uuid.UUID
	if req.GenerationID != nil {
		parsed, err := uuid.Parse(*req.GenerationID)
		if err != nil {
			soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_generation_id", "the generation id is not a uuid").WithCause(err))
			return
		}
		generationID = &parsed
	}

	message, err := c.service.Send(gc.Request.Context(), scope, conversationID, req.Text, generationID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, presentMessage(message))
}

type suggestionResponse struct {
	GenerationID string `json:"generation_id"`
	Text         string `json:"text"`
	CreatedAt    string `json:"created_at"`
}

// suggestion answers 204 when there is no draft waiting, which is not an error: most conversations have none (ADR-0046, 2.5).
func (c *Controller) suggestion(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return
	}

	s, err := c.service.Suggestion(gc.Request.Context(), scope, conversationID)
	if errors.Is(err, ErrNoSuggestion) {
		soniahttp.NoContent(gc)
		return
	}
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, suggestionResponse{
		GenerationID: s.GenerationID.String(),
		Text:         s.Text,
		CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339Nano),
	})
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("conversation_not_found", "no such conversation").WithCause(err)
	case errors.Is(err, ErrTaken):
		return soniahttp.Conflict("conversation_taken", "another operator is attending this conversation").WithCause(err)
	case errors.Is(err, ErrResolved):
		return soniahttp.Conflict("conversation_resolved", "the conversation is resolved").WithCause(err)
	case errors.Is(err, ErrWindowClosed):
		return soniahttp.Conflict("window_expired", "the 24 hour window closed: only a template can reach the contact now").WithCause(err)
	case errors.Is(err, ErrEmptyText):
		return soniahttp.BadRequest("invalid_text", "the answer cannot be empty").WithCause(err)
	case errors.Is(err, ErrUnknownMode):
		return soniahttp.BadRequest("invalid_mode", "the mode is not one of autonomous, suggestion, off").WithCause(err)
	case errors.Is(err, ErrSuggestionNotPending):
		return soniahttp.Conflict("suggestion_not_pending", "the suggestion is not a pending one of this conversation").WithCause(err)
	case errors.Is(err, ErrNoAIAgent):
		return soniahttp.Conflict("no_ai_agent", "the conversation has no agent to turn on").WithCause(err)
	case errors.Is(err, ErrAIAgentUnavailable):
		return soniahttp.Conflict("ai_agent_unavailable", "the agent of the conversation is paused or removed").WithCause(err)
	case errors.Is(err, ErrNotHeld):
		return soniahttp.Conflict("conversation_not_assigned", "only who holds the conversation switches its bot or closes it: answer it first").WithCause(err)
	case errors.Is(err, ErrTurnInProgress):
		return soniahttp.Conflict("turn_in_progress", "the bot is still answering this conversation: close it once the answer arrives").WithCause(err)
	case errors.Is(err, ErrInvalidScore):
		return soniahttp.BadRequest("invalid_score", "the score is a whole number from 1 to 5").WithCause(err)
	case errors.Is(err, ErrNotResolved):
		return soniahttp.Conflict("conversation_not_resolved", "only a resolved conversation is scored").WithCause(err)
	case errors.Is(err, ErrNotTheVendor):
		return soniahttp.Conflict("not_the_vendor", "only the vendor of the conversation scores it").WithCause(err)
	case errors.Is(err, ErrCannotReassign):
		return soniahttp.Forbidden("cannot_reassign", "only a supervisor or an admin reassigns conversations").WithCause(err)
	case errors.Is(err, ErrNoReason):
		return soniahttp.BadRequest("invalid_reason", "a reassignment needs a reason").WithCause(err)
	case errors.Is(err, ErrAlreadyAssigned):
		return soniahttp.Conflict("already_assigned", "the conversation is already assigned to that operator").WithCause(err)
	case errors.Is(err, ErrSameOperator):
		return soniahttp.BadRequest("same_operator", "the handover names the same operator twice").WithCause(err)
	case errors.Is(err, ErrOperatorNotFound):
		return soniahttp.NotFound("operator_not_found", "no such operator in the account").WithCause(err)
	case errors.Is(err, ErrOperatorUnavailable):
		return soniahttp.Conflict("operator_unavailable", "the operator is disabled or removed").WithCause(err)
	case errors.Is(err, ErrLabelNotFound):
		return soniahttp.NotFound("label_not_found", "no such label in the catalogue").WithCause(err)
	case errors.Is(err, ErrChannelUnavailable):
		return soniahttp.Conflict("channel_unavailable", "the number of this conversation cannot send: it does not exist or is not connected").WithCause(err)
	default:
		return err
	}
}

// scopeFrom builds what the listing narrows by, out of the actor and never out of the request.
func (c *Controller) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role}, true
}
