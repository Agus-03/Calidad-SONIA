package inbox

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// The same page size and ceiling as contacts/, so the two listings differ in how they continue and in nothing else.
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
	// Null at the end, and never an empty string: a frontend that sends "" back would be asking for the first page again.
	NextCursor *string `json:"next_cursor"`
}

type summaryResponse struct {
	ID              string            `json:"id"`
	Status          string            `json:"status"`
	Mode            string            `json:"mode"`
	AIAgentID       *string           `json:"ai_agent_id"`
	Contact         contactResponse   `json:"contact"`
	Operator        *operatorResponse `json:"operator"`
	LastMessage     *messageResponse  `json:"last_message"`
	FailedCount     int               `json:"failed_count"`
	LastFailedAt    *string           `json:"last_failed_at"`
	LastInboundAt   string            `json:"last_inbound_at"`
	WindowExpiresAt string            `json:"window_expires_at"`
	LastActivityAt  string            `json:"last_activity_at"`
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

func present(s Summary) summaryResponse {
	out := summaryResponse{
		ID:     s.ID.String(),
		Status: string(s.Status),
		Mode:   string(s.Mode),
		Contact: contactResponse{
			ID: s.Contact.ID.String(), Name: s.Contact.Name, Lastname: s.Contact.Lastname, PhoneWhatsapp: s.Contact.PhoneWhatsapp,
		},
		FailedCount:     s.FailedCount,
		LastFailedAt:    rfc3339(s.LastFailedAt),
		LastInboundAt:   s.LastInboundAt.UTC().Format(time.RFC3339),
		WindowExpiresAt: s.WindowExpiresAt.UTC().Format(time.RFC3339),
		LastActivityAt:  s.LastActivityAt.UTC().Format(time.RFC3339),
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

	page, err := c.service.List(gc.Request.Context(), scope, after, limit)
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

type sendRequest struct {
	Text string `json:"text" binding:"required"`
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
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a text is required").WithCause(err))
		return
	}

	message, err := c.service.Send(gc.Request.Context(), scope, conversationID, req.Text)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, presentMessage(message))
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

func limitFrom(gc *gin.Context) (int, error) {
	raw := gc.Query("limit")
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, soniahttp.BadRequest("invalid_limit", "limit must be a positive whole number").WithCause(err)
	}
	// Clamped and not refused, the same as per_page in contacts/: asking for too many is not a mistake worth failing the page over.
	if n > maxLimit {
		n = maxLimit
	}
	return n, nil
}

// cursorFrom reads the token the previous page handed out. An absent one is the first page; a malformed one is refused, because
// guessing would answer the first page again and the frontend would draw it as the next.
func cursorFrom(gc *gin.Context) (*Cursor, error) {
	raw := gc.Query("cursor")
	if raw == "" {
		return nil, nil
	}
	cursor, err := decodeCursor(raw)
	if err != nil {
		return nil, soniahttp.BadRequest("invalid_cursor", "the cursor is not one this listing handed out").WithCause(err)
	}
	return &cursor, nil
}

var errMalformedCursor = errors.New("inbox: malformed cursor")

// The cursor is opaque to the frontend on purpose: what it carries is how the query continues, and a client that learned to build one
// would break the day the ordering changes. Nanoseconds so the instant survives the round trip exactly and the comparison stays strict.
func encodeCursor(c Cursor) string {
	raw := c.LastActivityAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(token string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, err
	}
	at, rowID, found := strings.Cut(string(raw), "|")
	if !found {
		return Cursor{}, errMalformedCursor
	}
	lastActivity, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return Cursor{}, err
	}
	conversationID, err := uuid.Parse(rowID)
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{LastActivityAt: lastActivity, ID: conversationID}, nil
}
