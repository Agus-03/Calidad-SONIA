package contacts

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type createRequest struct {
	PhoneWhatsapp string `json:"phone_whatsapp" binding:"required"`
	Name          string `json:"name" binding:"required"`
	Lastname      string `json:"lastname" binding:"required"`
	Email         string `json:"email"`
	Type          string `json:"type" binding:"required"`
	Notes         string `json:"notes"`
	SourceChannel string `json:"source_channel" binding:"required"`

	DNI  string `json:"dni"`
	CUIT string `json:"cuit"`
	CUIL string `json:"cuil"`
	CBU  string `json:"cbu"`
}

func (c *Controller) create(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}

	var req createRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("invalid_body", "a phone, a name, a lastname, a type and a source channel are required").WithCause(err))
		return
	}

	contactType := enums.ContactType(req.Type)
	if !contactType.IsValid() {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_type", "the type is not one of client, provider, prospect"))
		return
	}
	source := enums.SourceChannel(req.SourceChannel)
	if !source.IsValid() {
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("invalid_source_channel", "the source channel is not one of meta_ads, qr, referral, direct"))
		return
	}

	contact, err := c.service.Create(gc.Request.Context(), scope, New{
		PhoneWhatsapp: req.PhoneWhatsapp, Name: req.Name, Lastname: req.Lastname,
		Email: req.Email, Type: contactType, Notes: req.Notes, SourceChannel: source,
		Sensitive: Sensitive{DNI: req.DNI, CUIT: req.CUIT, CUIL: req.CUIL, CBU: req.CBU},
	})
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	// Nil and not a read: a contact created a line ago has been tagged by nobody and talked in no conversation, so the empty
	// lists are the true answer.
	soniahttp.Created(gc, presentFor(scope.Role, contact, nil, nil))
}

// update reads the body as raw keys first, because which fields arrived is the question: a struct cannot tell a field left out
// from a field sent empty, and refusing on a zero value would refuse every partial edit (ADR-0032, regla 14).
func (c *Controller) update(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var raw map[string]json.RawMessage
	if err := gc.ShouldBindJSON(&raw); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "the body is not a json object").WithCause(err))
		return
	}

	sent := make([]string, 0, len(raw))
	for field := range raw {
		sent = append(sent, field)
	}
	// Known first, writable second: the order is the difference between telling a caller they made a typo and telling them a field exists.
	if err := CheckKnown(sent); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	if err := CheckWritable(scope.Role, sent); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	edit, err := applier(raw)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	contact, err := c.service.Apply(gc.Request.Context(), scope, contactID, edit)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	body, ok := c.card(gc, scope, contact)
	if !ok {
		return
	}
	soniahttp.OK(gc, body)
}

// applier decodes only the keys that arrived, so what nobody sent keeps the value the row already had.
func applier(raw map[string]json.RawMessage) (func(*Changes), error) {
	var name, lastname, email, contactType, notes string
	var optOut, blocked bool

	strings := map[string]*string{
		FieldName: &name, FieldLastname: &lastname, FieldEmail: &email, FieldType: &contactType, FieldNotes: &notes,
	}
	flags := map[string]*bool{FieldOptOut: &optOut, FieldBlocked: &blocked}

	for field, value := range raw {
		if into, ok := strings[field]; ok {
			if err := json.Unmarshal(value, into); err != nil {
				return nil, soniahttp.BadRequest("invalid_body", "the field "+field+" is not a string").WithCause(err)
			}
			continue
		}
		if into, ok := flags[field]; ok {
			if err := json.Unmarshal(value, into); err != nil {
				return nil, soniahttp.BadRequest("invalid_body", "the field "+field+" is not true or false").WithCause(err)
			}
			continue
		}
		// Unreachable today: CheckKnown already refused what is not a contact field, and CheckWritable refused every known one this
		// applier has no slot for. It stays as the guard for the edit that adds a field to those two tables and forgets this one,
		// because the failure it would otherwise cause is silence — the field arrives, nothing writes it, and the caller is told it saved.
		return nil, soniahttp.BadRequest("unknown_field", "the field "+field+" is not part of a contact")
	}

	return func(ch *Changes) {
		if _, ok := raw[FieldName]; ok {
			ch.Name = name
		}
		if _, ok := raw[FieldLastname]; ok {
			ch.Lastname = lastname
		}
		if _, ok := raw[FieldEmail]; ok {
			ch.Email = email
		}
		if _, ok := raw[FieldType]; ok {
			ch.Type = enums.ContactType(contactType)
		}
		if _, ok := raw[FieldNotes]; ok {
			ch.Notes = notes
		}
		if _, ok := raw[FieldOptOut]; ok {
			ch.OptOut = optOut
		}
		if _, ok := raw[FieldBlocked]; ok {
			ch.Blocked = blocked
		}
	}, nil
}

type sensitiveRequest struct {
	DNI  string `json:"dni"`
	CUIT string `json:"cuit"`
	CUIL string `json:"cuil"`
	CBU  string `json:"cbu"`
}

// writeSensitive takes the four together and replaces all four, which is what makes taking one away possible: a partial write here
// would mean a field could never be cleared (ADR-0032, regla 9).
func (c *Controller) writeSensitive(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var req sensitiveRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "the body is not a json object").WithCause(err))
		return
	}

	contact, err := c.service.UpdateSensitive(gc.Request.Context(), scope, contactID,
		Sensitive{DNI: req.DNI, CUIT: req.CUIT, CUIL: req.CUIL, CBU: req.CBU})
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	body, ok := c.card(gc, scope, contact)
	if !ok {
		return
	}
	soniahttp.OK(gc, body)
}
