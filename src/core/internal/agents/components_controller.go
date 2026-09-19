package agents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The fields a component answers to in JSON. The type and the position are written once, on create: the panel sends the order
// prompting asks for, and it does not move after (ADR-0039).
const (
	fieldType       = "type"
	fieldContent    = "content"
	fieldPosition   = "position"
	fieldChangeNote = "change_note"
)

var (
	componentCreateFields = []string{fieldType, fieldContent, fieldPosition, fieldChangeNote}
	componentUpdateFields = []string{fieldContent, fieldActive, fieldChangeNote}
)

type ComponentsController struct {
	service *Components
	log     *slog.Logger
}

func NewComponentsController(service *Components, log *slog.Logger) *ComponentsController {
	if log == nil {
		log = slog.Default()
	}
	return &ComponentsController{service: service, log: log}
}

type componentResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	Position  int16  `json:"position"`
	Active    bool   `json:"active"`
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type componentListResponse struct {
	Items []componentResponse `json:"items"`
}

type versionResponse struct {
	Version     int     `json:"version"`
	Content     string  `json:"content"`
	Active      bool    `json:"active"`
	ChangedByID *string `json:"changed_by_operator_id"`
	ChangeNote  *string `json:"change_note"`
	CreatedAt   string  `json:"created_at"`
}

type versionListResponse struct {
	Items []versionResponse `json:"items"`
}

func presentComponent(c Component) componentResponse {
	return componentResponse{
		ID: c.ID.String(), Type: string(c.Type), Content: c.Content, Position: c.Position, Active: c.Active, Version: c.Version,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *ComponentsController) list(gc *gin.Context) {
	actor, agentID, ok := c.scope(gc)
	if !ok {
		return
	}
	found, err := c.service.List(gc.Request.Context(), actor.AccountID, agentID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateComponent(err))
		return
	}
	items := make([]componentResponse, 0, len(found))
	for _, one := range found {
		items = append(items, presentComponent(one))
	}
	soniahttp.OK(gc, componentListResponse{Items: items})
}

func (c *ComponentsController) create(gc *gin.Context) {
	actor, agentID, ok := c.scope(gc)
	if !ok {
		return
	}
	raw, err := body(gc, componentCreateFields)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	var n NewComponent
	var kind string
	for _, err := range []error{
		required(raw, fieldType, &kind),
		required(raw, fieldContent, &n.Content),
		required(raw, fieldPosition, &n.Position),
		optional(raw, fieldChangeNote, &n.ChangeNote),
	} {
		if err != nil {
			soniahttp.Fail(gc, c.log, err)
			return
		}
	}
	n.Type = enums.PromptComponentType(kind)

	created, err := c.service.Create(gc.Request.Context(), actor.AccountID, actor.OperatorID, agentID, n)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateComponent(err))
		return
	}
	soniahttp.Created(gc, presentComponent(created))
}

// update reads the body as raw keys, the same as an agent's: which fields arrived is the question.
func (c *ComponentsController) update(gc *gin.Context) {
	actor, agentID, ok := c.scope(gc)
	if !ok {
		return
	}
	componentID, err := componentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	raw, err := body(gc, componentUpdateFields)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	var e ComponentEdit
	for _, err := range []error{
		optional(raw, fieldContent, &e.Content),
		optional(raw, fieldActive, &e.Active),
		optional(raw, fieldChangeNote, &e.ChangeNote),
	} {
		if err != nil {
			soniahttp.Fail(gc, c.log, err)
			return
		}
	}
	// A null content or switch is not a value the row can hold; only the note clears with null.
	_, contentSent := raw[fieldContent]
	_, activeSent := raw[fieldActive]
	if (contentSent && e.Content == nil) || (activeSent && e.Active == nil) {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "content and active cannot be null"))
		return
	}

	updated, err := c.service.Update(gc.Request.Context(), actor.AccountID, actor.OperatorID, agentID, componentID, e)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateComponent(err))
		return
	}
	soniahttp.OK(gc, presentComponent(updated))
}

func (c *ComponentsController) remove(gc *gin.Context) {
	actor, agentID, ok := c.scope(gc)
	if !ok {
		return
	}
	componentID, err := componentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	if err := c.service.Delete(gc.Request.Context(), actor.AccountID, actor.OperatorID, agentID, componentID); err != nil {
		soniahttp.Fail(gc, c.log, translateComponent(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *ComponentsController) versions(gc *gin.Context) {
	actor, agentID, ok := c.scope(gc)
	if !ok {
		return
	}
	componentID, err := componentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	found, err := c.service.Versions(gc.Request.Context(), actor.AccountID, agentID, componentID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateComponent(err))
		return
	}
	items := make([]versionResponse, 0, len(found))
	for _, v := range found {
		drawn := versionResponse{
			Version: v.Number, Content: v.Content, Active: v.Active, ChangeNote: v.ChangeNote,
			CreatedAt: v.CreatedAt.UTC().Format(time.RFC3339),
		}
		if v.ChangedBy != nil {
			by := v.ChangedBy.String()
			drawn.ChangedByID = &by
		}
		items = append(items, drawn)
	}
	soniahttp.OK(gc, versionListResponse{Items: items})
}

// scope reads who asks and for which agent, the two things every route of a component starts from.
func (c *ComponentsController) scope(gc *gin.Context) (soniahttp.Actor, uuid.UUID, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return soniahttp.Actor{}, uuid.Nil, false
	}
	agentID, err := agentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return soniahttp.Actor{}, uuid.Nil, false
	}
	return actor, agentID, true
}

// required decodes a field that has to arrive and cannot be null.
func required(raw map[string]json.RawMessage, field string, into any) error {
	value, sent := raw[field]
	if !sent || string(value) == "null" {
		return soniahttp.BadRequest("invalid_body", "type, content and position are required")
	}
	if err := json.Unmarshal(value, into); err != nil {
		return soniahttp.BadRequest("invalid_body", "the field "+field+" has the wrong type").WithCause(err)
	}
	return nil
}

// optional decodes a field only when it arrived; into is a pointer to a pointer, so a null leaves it nil.
func optional(raw map[string]json.RawMessage, field string, into any) error {
	value, sent := raw[field]
	if !sent {
		return nil
	}
	if err := json.Unmarshal(value, into); err != nil {
		return soniahttp.BadRequest("invalid_body", "the field "+field+" has the wrong type").WithCause(err)
	}
	return nil
}

// translateComponent is translate plus what only a component can say.
func translateComponent(err error) error {
	switch {
	case errors.Is(err, ErrComponentNotFound):
		return soniahttp.NotFound("prompt_component_not_found", "no such prompt component").WithCause(err)
	case errors.Is(err, ErrUnknownComponentType):
		return soniahttp.BadRequest("invalid_component_type",
			"the type is not one of objective, personality, behavior, tone, brand_voice, rules").WithCause(err)
	case errors.Is(err, ErrEmptyContent):
		return soniahttp.BadRequest("invalid_content", "the content cannot be empty").WithCause(err)
	case errors.Is(err, ErrInvalidPosition):
		return soniahttp.BadRequest("invalid_position", "the position has to be a whole number between 1 and 32767").WithCause(err)
	case errors.Is(err, ErrComponentTypeTaken):
		return soniahttp.Conflict("component_type_taken", "the ai agent already has a component of that type: edit it").WithCause(err)
	case errors.Is(err, ErrComponentPositionTaken):
		return soniahttp.Conflict("component_position_taken", "the ai agent already has a component at that position").WithCause(err)
	default:
		return translate(err)
	}
}

func componentIDFrom(gc *gin.Context) (uuid.UUID, error) {
	componentID, err := uuid.Parse(gc.Param("componentId"))
	if err != nil {
		return uuid.Nil, soniahttp.BadRequest("invalid_prompt_component_id", "the prompt component id is not a uuid").WithCause(err)
	}
	return componentID, nil
}
