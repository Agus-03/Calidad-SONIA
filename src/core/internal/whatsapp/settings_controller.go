package whatsapp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The one field of an inbox the panel writes today: connecting a number is still --connect-inbox (ADR-0033), and the rest of the
// row is Fase 13's or nobody's yet.
const fieldDefaultAIAgentID = "default_ai_agent_id"

// SettingsController is the admin panel of the numbers, under /api/v1 and behind a role, unlike Controller, which Meta calls.
type SettingsController struct {
	settings *Settings
	log      *slog.Logger
}

func NewSettingsController(settings *Settings, log *slog.Logger) *SettingsController {
	if log == nil {
		log = slog.Default()
	}
	return &SettingsController{settings: settings, log: log}
}

type inboxResponse struct {
	ID               string  `json:"id"`
	DisplayName      string  `json:"display_name"`
	PhoneNumberID    string  `json:"phone_number_id"`
	Status           string  `json:"status"`
	DefaultAIAgentID *string `json:"default_ai_agent_id"`
}

type inboxListResponse struct {
	Items []inboxResponse `json:"items"`
}

func presentInbox(s InboxSettings) inboxResponse {
	out := inboxResponse{ID: s.ID.String(), DisplayName: s.DisplayName, PhoneNumberID: s.PhoneNumberID, Status: string(s.Status)}
	if s.DefaultAIAgentID != nil {
		agent := s.DefaultAIAgentID.String()
		out.DefaultAIAgentID = &agent
	}
	return out
}

func (c *SettingsController) list(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	found, err := c.settings.List(gc.Request.Context(), actor.AccountID)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	items := make([]inboxResponse, 0, len(found))
	for _, s := range found {
		items = append(items, presentInbox(s))
	}
	soniahttp.OK(gc, inboxListResponse{Items: items})
}

// CAMBIADO POR TOBI
type createInboxRequest struct {
	InboxName         string `json:"inbox_name" binding:"required"`
	PhoneNumber       string `json:"phone_number"` // May not be mapped to DB
	PhoneNumberID     string `json:"phone_number_id" binding:"required"`
	BusinessAccountID string `json:"business_account_id" binding:"required"`
	APIKey            string `json:"api_key" binding:"required"`
}

func (c *SettingsController) create(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}

	var req createInboxRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "invalid payload").WithCause(err))
		return
	}

	// Verify credentials with Meta API before creating
	// WhatsApp Cloud API endpoints usually respond to GET /<PHONE_NUMBER_ID> with basic info if the token is valid.
	c.log.Info("Executing Meta API validation", "phone_number_id", req.PhoneNumberID)
	verifyURL := "https://graph.facebook.com/v19.0/" + req.PhoneNumberID
	httpReq, err := http.NewRequestWithContext(gc.Request.Context(), http.MethodGet, verifyURL, nil)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.InternalServerError("verification_failed", "could not create verification request").WithCause(err))
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.InternalServerError("verification_failed", "could not reach meta api").WithCause(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_credentials", "the provided phone number ID or API key are invalid"))
		return
	}

	var metaResp struct {
		ID                 string `json:"id"`
		DisplayPhoneNumber string `json:"display_phone_number"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&metaResp); err != nil || metaResp.DisplayPhoneNumber == "" {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_credentials", "the provided ID is not a valid WhatsApp Phone Number ID"))
		return
	}

	// Generate a verification token for Meta webhook
	verifyToken := uuid.New().String()

	inbox, err := c.settings.Create(
		gc.Request.Context(), 
		actor.AccountID, 
		req.InboxName, 
		req.PhoneNumberID, 
		req.BusinessAccountID, 
		req.APIKey, 
		verifyToken,
	)
	if err != nil {
		if errors.Is(err, ErrPhoneNumberIDInUse) {
			soniahttp.Fail(gc, c.log, soniahttp.Conflict("phone_number_in_use", "this phone number ID is already registered").WithCause(err))
			return
		}
		soniahttp.Fail(gc, c.log, err)
		return
	}
	soniahttp.Created(gc, presentInbox(inbox))
}

// setDefault reads the body as raw keys, because null is the value that leaves the number without AI and a struct cannot tell it
// from a field nobody sent.
func (c *SettingsController) setDefault(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	inboxID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_inbox_id", "the inbox id is not a uuid").WithCause(err))
		return
	}
	aiAgentID, err := defaultAIAgentFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	updated, err := c.settings.SetDefaultAIAgent(gc.Request.Context(), actor.AccountID, actor.OperatorID, inboxID, aiAgentID)
	switch {
	case errors.Is(err, ErrInboxNotFound):
		soniahttp.Fail(gc, c.log, soniahttp.NotFound("inbox_not_found", "no such inbox").WithCause(err))
	case errors.Is(err, ErrAIAgentNotAssignable):
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("ai_agent_not_assignable", "the ai agent does not exist or was removed").WithCause(err))
	case err != nil:
		soniahttp.Fail(gc, c.log, err)
	default:
		soniahttp.OK(gc, presentInbox(updated))
	}
}

// defaultAIAgentFrom takes exactly one key: anything else of an inbox is not writable here, and saying so by name tells a typo apart
// from a field that saved.
func defaultAIAgentFrom(gc *gin.Context) (*uuid.UUID, error) {
	var raw map[string]json.RawMessage
	if err := gc.ShouldBindJSON(&raw); err != nil {
		return nil, soniahttp.BadRequest("invalid_body", "the body is not a json object").WithCause(err)
	}
	for field := range raw {
		if field != fieldDefaultAIAgentID {
			return nil, soniahttp.BadRequest("unknown_field", "the field "+field+" cannot be written on an inbox")
		}
	}
	value, sent := raw[fieldDefaultAIAgentID]
	if !sent {
		return nil, soniahttp.BadRequest("invalid_body", "default_ai_agent_id is required, null to leave the number without AI")
	}

	var text *string
	if err := json.Unmarshal(value, &text); err != nil {
		return nil, soniahttp.BadRequest("invalid_body", "default_ai_agent_id has to be a uuid or null").WithCause(err)
	}
	if text == nil {
		return nil, nil
	}
	aiAgentID, err := uuid.Parse(*text)
	if err != nil {
		return nil, soniahttp.BadRequest("invalid_ai_agent_id", "default_ai_agent_id is not a uuid").WithCause(err)
	}
	return &aiAgentID, nil
}

func (c *SettingsController) remove(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	inboxID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_inbox_id", "the inbox id is not a uuid").WithCause(err))
		return
	}

	err = c.settings.Remove(gc.Request.Context(), actor.AccountID, inboxID)
	if errors.Is(err, ErrInboxNotFound) {
		soniahttp.Fail(gc, c.log, soniahttp.NotFound("inbox_not_found", "no such inbox").WithCause(err))
		return
	}
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	gc.Status(http.StatusNoContent)
}

type setAgentsRequest struct {
	AgentIDs []string `json:"agent_ids" binding:"required"`
}

func (c *SettingsController) setAgents(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	inboxID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_inbox_id", "the inbox id is not a uuid").WithCause(err))
		return
	}

	var req setAgentsRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "invalid payload").WithCause(err))
		return
	}

	operatorIDs := make([]uuid.UUID, 0, len(req.AgentIDs))
	for _, a := range req.AgentIDs {
		id, err := uuid.Parse(a)
		if err != nil {
			soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_agent_id", "agent id is not a uuid: "+a).WithCause(err))
			return
		}
		operatorIDs = append(operatorIDs, id)
	}

	err = c.settings.SetAgents(gc.Request.Context(), actor.AccountID, inboxID, operatorIDs)
	if errors.Is(err, ErrInboxNotFound) {
		soniahttp.Fail(gc, c.log, soniahttp.NotFound("inbox_not_found", "no such inbox").WithCause(err))
		return
	}
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	gc.Status(http.StatusNoContent)
}

func (c *SettingsController) actor(gc *gin.Context) (soniahttp.Actor, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
	}
	return actor, ok
}
