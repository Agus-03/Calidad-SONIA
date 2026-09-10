package contacts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// A page nobody asked to size, and a ceiling so per_page=100000 is not a query the caller can ask for.
	defaultPerPage = 20
	maxPerPage     = 100
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

// contactResponse is what every role gets. The four sensitive fields are not here and not masked here: a masked value still
// travels in the JSON, one keystroke away from being read (ADR-0032, regla 4).
type contactResponse struct {
	ID               string  `json:"id"`
	PhoneWhatsapp    string  `json:"phone_whatsapp"`
	Name             string  `json:"name"`
	Lastname         string  `json:"lastname"`
	Email            string  `json:"email"`
	Type             string  `json:"type"`
	Notes            string  `json:"notes"`
	SourceChannel    string  `json:"source_channel"`
	ConsentGivenAt   *string `json:"consent_given_at"`
	ConsentRevokedAt *string `json:"consent_revoked_at"`
	OptOut           bool    `json:"opt_out"`
	Blocked          bool    `json:"blocked"`
}

// labelResponse is the catalogue entry as the card carries it: what it takes to draw a chip and nothing more. Who tagged and
// when live in contact_labels and are not part of the card (ADR-0003).
type labelResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// cardResponse is one contact with its labels. The listing does not use it, and that is deliberate: a page of two hundred rows
// would be two hundred catalogue reads, so the row type has no labels field rather than an empty one that reads as untagged.
type cardResponse struct {
	contactResponse
	Labels []labelResponse `json:"labels"`
}

// sensitiveResponse embeds the card and adds the four. It is a second type and not a flag on the first, because a role that must
// not see them is served by a struct that has nowhere to put them: nothing is omitted at render time, so nothing can be forgotten.
type sensitiveResponse struct {
	cardResponse
	DNI  string `json:"dni"`
	CUIT string `json:"cuit"`
	CUIL string `json:"cuil"`
	CBU  string `json:"cbu"`
}

type pageResponse struct {
	Items   []contactResponse `json:"items"`
	Total   int               `json:"total"`
	Page    int               `json:"page"`
	PerPage int               `json:"per_page"`
}

func present(c Contact) contactResponse {
	return contactResponse{
		ID: c.ID.String(), PhoneWhatsapp: c.PhoneWhatsapp,
		Name: c.Name, Lastname: c.Lastname, Email: c.Email,
		Type: string(c.Type), Notes: c.Notes, SourceChannel: string(c.SourceChannel),
		ConsentGivenAt: rfc3339(c.ConsentGivenAt), ConsentRevokedAt: rfc3339(c.ConsentRevokedAt),
		OptOut: c.OptOut, Blocked: c.Blocked,
	}
}

// presentFor is the single place the rule is applied, so adding a handler cannot forget it: the role decides which struct is built.
func presentFor(role enums.OperatorRole, c Contact, tags []labels.Label) any {
	chips := make([]labelResponse, 0, len(tags))
	for _, t := range tags {
		chips = append(chips, labelResponse{ID: t.ID.String(), Name: t.Name, Color: t.Color})
	}
	card := cardResponse{contactResponse: present(c), Labels: chips}

	if !seesSensitive(role) {
		return card
	}
	return sensitiveResponse{
		cardResponse: card,
		DNI:          c.Sensitive.DNI, CUIT: c.Sensitive.CUIT, CUIL: c.Sensitive.CUIL, CBU: c.Sensitive.CBU,
	}
}

// The two roles named by ADR-0032 regla 4, and nothing else: an unknown role falls to the side that shows less.
func seesSensitive(role enums.OperatorRole) bool {
	return role == enums.OperatorRoleSupervisor || role == enums.OperatorRoleAdmin
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

	page, perPage, err := pagination(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	found, err := c.service.List(gc.Request.Context(), scope, perPage, (page-1)*perPage)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}

	// The listing never carries the four, for any role: the repository does not even select them, and the card is where they belong.
	items := make([]contactResponse, 0, len(found.Items))
	for _, one := range found.Items {
		items = append(items, present(one))
	}
	soniahttp.OK(gc, pageResponse{Items: items, Total: found.Total, Page: page, PerPage: perPage})
}

func (c *Controller) show(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	contact, err := c.service.ByID(gc.Request.Context(), scope, contactID)
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
	// Nil and not a read: a contact created a line ago has been tagged by nobody, so the empty list is the true answer.
	soniahttp.Created(gc, presentFor(scope.Role, contact, nil))
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

type assignLabelRequest struct {
	LabelID string `json:"label_id" binding:"required"`
}

// assignLabel answers the whole card and not just the tag, because the caller drew a card and what changed is on it.
// Tagging twice is not refused: the request says what the contact should carry, and after the second one it carries it.
func (c *Controller) assignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var req assignLabelRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a label_id is required").WithCause(err))
		return
	}
	labelID, err := uuid.Parse(req.LabelID)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the label id is not a uuid").WithCause(err))
		return
	}

	if err := c.service.AssignLabel(gc.Request.Context(), scope, contactID, labelID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	c.respondWithCard(gc, scope, contactID)
}

// unassignLabel takes the label from the path and not from a body, because a DELETE identifies what it removes by its address.
func (c *Controller) unassignLabel(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	labelID, err := uuid.Parse(gc.Param("label_id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the label id is not a uuid").WithCause(err))
		return
	}

	if err := c.service.UnassignLabel(gc.Request.Context(), scope, contactID, labelID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	c.respondWithCard(gc, scope, contactID)
}

// respondWithCard re-reads the contact so the two label handlers answer the state that is now stored, and not the one they
// assumed they left behind.
func (c *Controller) respondWithCard(gc *gin.Context, scope Scope, contactID uuid.UUID) {
	contact, err := c.service.ByID(gc.Request.Context(), scope, contactID)
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

// card is the one place a single contact becomes a response, so a handler cannot ship a card whose labels were never read.
func (c *Controller) card(gc *gin.Context, scope Scope, contact Contact) (any, bool) {
	tags, err := c.service.LabelsOf(gc.Request.Context(), scope, contact.ID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return nil, false
	}
	return presentFor(scope.Role, contact, tags), true
}

func (c *Controller) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	contactID, err := contactIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	if err := c.service.Delete(gc.Request.Context(), scope, contactID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// scopeFrom builds what every read narrows by, out of the actor and never out of the request (ADR-0032, regla 2).
func (c *Controller) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role}, true
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	var notWritable ErrFieldNotWritable
	var unknown ErrUnknownField
	switch {
	case errors.As(err, &unknown):
		return soniahttp.BadRequest("unknown_field", unknown.Field+" is not part of a contact").
			WithDetails(map[string]string{"field": unknown.Field}).WithCause(err)
	case errors.As(err, &notWritable):
		return soniahttp.Forbidden("field_not_writable", "this role cannot write "+notWritable.Field).
			WithDetails(map[string]string{"field": notWritable.Field}).WithCause(err)
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("contact_not_found", "no such contact").WithCause(err)
	case errors.Is(err, ErrLabelNotInAccount):
		return soniahttp.NotFound("label_not_found", "no such label in the catalogue").WithCause(err)
	case errors.Is(err, ErrDuplicatePhone):
		return soniahttp.Conflict("phone_taken", "a contact with that phone already exists").WithCause(err)
	case errors.Is(err, ErrEmptyName):
		return soniahttp.BadRequest("invalid_name", "the name and the lastname cannot be empty").WithCause(err)
	case errors.Is(err, ErrEmptyPhone):
		return soniahttp.BadRequest("invalid_phone", "the phone must carry at least one digit").WithCause(err)
	default:
		return err
	}
}

func contactIDFrom(gc *gin.Context) (uuid.UUID, error) {
	contactID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		return uuid.Nil, soniahttp.BadRequest("invalid_id", "the contact id is not a uuid").WithCause(err)
	}
	return contactID, nil
}

// A page past the end answers an empty list with the real total, because "there is nothing here" is not the same as "this does not exist".
func pagination(gc *gin.Context) (page, perPage int, err error) {
	page, err = positive(gc.Query("page"), 1)
	if err != nil {
		return 0, 0, soniahttp.BadRequest("invalid_page", "page must be a positive whole number").WithCause(err)
	}
	perPage, err = positive(gc.Query("per_page"), defaultPerPage)
	if err != nil {
		return 0, 0, soniahttp.BadRequest("invalid_per_page", "per_page must be a positive whole number").WithCause(err)
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage, nil
}

func positive(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, errors.New("contacts: not a positive whole number")
	}
	return n, nil
}
