package agents

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The fields an agent answers to in JSON. default_mode and extraction_enabled are not among them until Fase 9, so sending them is
// refused as unknown instead of saved without effect (ADR-0038).
const (
	fieldName        = "name"
	fieldDescription = "description"
	fieldLLMProvider = "llm_provider"
	fieldLLMModel    = "llm_model"
	fieldTemperature = "temperature"
	fieldMaxTokens   = "max_tokens"
	fieldActive      = "active"
)

// active is not writable on create: a new agent is born active, and pausing it is an edit with an audit row of its own.
var (
	createFields = []string{fieldName, fieldDescription, fieldLLMProvider, fieldLLMModel, fieldTemperature, fieldMaxTokens}
	updateFields = append([]string{fieldActive}, createFields...)
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

type agentResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description *string  `json:"description"`
	LLMProvider string   `json:"llm_provider"`
	LLMModel    string   `json:"llm_model"`
	Temperature *float64 `json:"temperature"`
	MaxTokens   *int     `json:"max_tokens"`
	Active      bool     `json:"active"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type listResponse struct {
	Items []agentResponse `json:"items"`
}

func present(a AIAgent) agentResponse {
	return agentResponse{
		ID: a.ID.String(), Name: a.Name, Description: a.Description, LLMProvider: a.LLMProvider, LLMModel: a.LLMModel,
		Temperature: a.Temperature, MaxTokens: a.MaxTokens, Active: a.Active,
		CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Controller) list(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	found, err := c.service.List(gc.Request.Context(), actor.AccountID)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	items := make([]agentResponse, 0, len(found))
	for _, a := range found {
		items = append(items, present(a))
	}
	soniahttp.OK(gc, listResponse{Items: items})
}

func (c *Controller) show(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	agentID, err := agentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	agent, err := c.service.Get(gc.Request.Context(), actor.AccountID, agentID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(agent))
}

func (c *Controller) create(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	raw, err := body(gc, createFields)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	for _, required := range []string{fieldName, fieldLLMProvider, fieldLLMModel} {
		if _, sent := raw[required]; !sent {
			soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "name, llm_provider and llm_model are required"))
			return
		}
	}
	edit, err := applier(raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var config Config
	edit(&config)
	agent, err := c.service.Create(gc.Request.Context(), actor.AccountID, actor.OperatorID, config)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(agent))
}

// update reads the body as raw keys, because which fields arrived is the question: a struct cannot tell a description left out from
// one sent as null to clear it (the same reading as contacts/, ADR-0032 regla 14).
func (c *Controller) update(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	agentID, err := agentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	raw, err := body(gc, updateFields)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	edit, err := applier(raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	agent, err := c.service.Update(gc.Request.Context(), actor.AccountID, actor.OperatorID, agentID, edit)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(agent))
}

func (c *Controller) remove(gc *gin.Context) {
	actor, ok := c.actor(gc)
	if !ok {
		return
	}
	agentID, err := agentIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	if err := c.service.Delete(gc.Request.Context(), actor.AccountID, actor.OperatorID, agentID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *Controller) actor(gc *gin.Context) (soniahttp.Actor, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
	}
	return actor, ok
}

// body refuses a field that is not writable here by name, so a typo or a Fase 9 field is told apart from a field that saved.
func body(gc *gin.Context, allowed []string) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := gc.ShouldBindJSON(&raw); err != nil {
		return nil, soniahttp.BadRequest("invalid_body", "the body is not a json object").WithCause(err)
	}
	for field := range raw {
		known := false
		for _, a := range allowed {
			known = known || a == field
		}
		if !known {
			return nil, soniahttp.BadRequest("unknown_field", "the field "+field+" cannot be written on an ai agent")
		}
	}
	return raw, nil
}

// applier decodes only the keys that arrived. The three nullable columns take null to clear them; the rest refuse it, because a
// null name or model is not a value the row can hold.
func applier(raw map[string]json.RawMessage) (func(*Config), error) {
	var edits []func(*Config)

	text := func(field string, set func(*Config, string)) error {
		value, sent := raw[field]
		if !sent {
			return nil
		}
		var s *string
		if err := json.Unmarshal(value, &s); err != nil || s == nil {
			return soniahttp.BadRequest("invalid_body", "the field "+field+" has to be a string").WithCause(err)
		}
		edits = append(edits, func(c *Config) { set(c, *s) })
		return nil
	}
	nullable := func(field string, into any, set func(*Config)) error {
		value, sent := raw[field]
		if !sent {
			return nil
		}
		if err := json.Unmarshal(value, into); err != nil {
			return soniahttp.BadRequest("invalid_body", "the field "+field+" has the wrong type").WithCause(err)
		}
		edits = append(edits, set)
		return nil
	}

	var description *string
	var temperature *float64
	var maxTokens *int
	var active *bool

	for _, err := range []error{
		text(fieldName, func(c *Config, s string) { c.Name = s }),
		text(fieldLLMProvider, func(c *Config, s string) { c.LLMProvider = s }),
		text(fieldLLMModel, func(c *Config, s string) { c.LLMModel = s }),
		nullable(fieldDescription, &description, func(c *Config) { c.Description = description }),
		nullable(fieldTemperature, &temperature, func(c *Config) { c.Temperature = temperature }),
		nullable(fieldMaxTokens, &maxTokens, func(c *Config) { c.MaxTokens = maxTokens }),
		nullable(fieldActive, &active, func(c *Config) { c.Active = *active }),
	} {
		if err != nil {
			return nil, err
		}
	}
	if _, sent := raw[fieldActive]; sent && active == nil {
		return nil, soniahttp.BadRequest("invalid_body", "the field active has to be true or false")
	}

	return func(c *Config) {
		for _, e := range edits {
			e(c)
		}
	}, nil
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("ai_agent_not_found", "no such ai agent").WithCause(err)
	case errors.Is(err, ErrEmptyName):
		return soniahttp.BadRequest("invalid_name", "the name cannot be empty").WithCause(err)
	case errors.Is(err, ErrEmptyModel):
		return soniahttp.BadRequest("invalid_model", "the provider and the model cannot be empty").WithCause(err)
	case errors.Is(err, ErrUnpricedModel):
		return soniahttp.BadRequest("unpriced_model", "the brain has no tariff for that provider and model").WithCause(err)
	case errors.Is(err, ErrInvalidTemperature):
		return soniahttp.BadRequest("invalid_temperature", "the temperature cannot be negative").WithCause(err)
	case errors.Is(err, ErrInvalidMaxTokens):
		return soniahttp.BadRequest("invalid_max_tokens", "max_tokens has to be a whole number between 1 and 2147483647").WithCause(err)
	case errors.Is(err, ErrChanged):
		return soniahttp.Conflict("ai_agent_changed", "another save changed this ai agent: reload it and try again").WithCause(err)
	case errors.Is(err, ErrModelsUnavailable):
		return soniahttp.Unavailable("models_unavailable", "the brain could not confirm the model, try again").WithCause(err)
	default:
		return err
	}
}

func agentIDFrom(gc *gin.Context) (uuid.UUID, error) {
	agentID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		return uuid.Nil, soniahttp.BadRequest("invalid_ai_agent_id", "the ai agent id is not a uuid").WithCause(err)
	}
	return agentID, nil
}
