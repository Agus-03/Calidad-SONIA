package contacts

import (
	"errors"
	"log/slog"
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

	// The same ceiling reasoning applied to the filter: a segment is a handful of labels, and fifty of them is not a question
	// anybody is asking but is a subquery anybody can send.
	maxLabelFilters = 20
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
	// What F31 found in the conversations, beside and never over what a person loaded (ADR-0047, sections 2.5 and 2.7).
	Attributes []attributeResponse `json:"attributes"`
}

type attributeResponse struct {
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Source     string   `json:"source"`
	Confidence *float64 `json:"confidence"`
	CreatedAt  string   `json:"created_at"`
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
func presentFor(role enums.OperatorRole, c Contact, tags []labels.Label, attributes []Attribute) any {
	chips := make([]labelResponse, 0, len(tags))
	for _, t := range tags {
		chips = append(chips, labelResponse{ID: t.ID.String(), Name: t.Name, Color: t.Color})
	}
	found := make([]attributeResponse, 0, len(attributes))
	for _, a := range attributes {
		found = append(found, attributeResponse{
			Key: a.Key, Value: a.Value, Source: string(a.Source), Confidence: a.Confidence,
			CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	card := cardResponse{contactResponse: present(c), Labels: chips, Attributes: found}

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
	filter, err := filterFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	found, err := c.service.List(gc.Request.Context(), scope, filter, perPage, (page-1)*perPage)
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
	attributes, err := c.service.AttributesOf(gc.Request.Context(), scope, contact.ID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return nil, false
	}
	return presentFor(scope.Role, contact, tags, attributes), true
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
	case errors.Is(err, ErrUnknownMatch):
		return soniahttp.BadRequest("invalid_match", "match must be all or any").WithCause(err)
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
