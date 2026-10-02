package operators

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// A page nobody asked to size, and a ceiling so per_page=100000 is not a query the caller can ask for, as in labels/.
	defaultPerPage = 20
	maxPerPage     = 100
)

// RosterController is apart from Controller because /me serves everybody and the roster is the admin's (ADR-0062).
type RosterController struct {
	roster *Roster
	log    *slog.Logger
}

func NewRosterController(roster *Roster, log *slog.Logger) *RosterController {
	if log == nil {
		log = slog.Default()
	}
	return &RosterController{roster: roster, log: log}
}

// The account is not a field: it comes from the Actor, so nobody registers a person in another account.
type createRequest struct {
	Name     string `json:"name" binding:"required"`
	Lastname string `json:"lastname" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role" binding:"required"`
}

// Pointers so a field left out is told apart from one sent empty: deactivating sends only active.
type updateRequest struct {
	Name     *string `json:"name"`
	Lastname *string `json:"lastname"`
	Email    *string `json:"email"`
	Role     *string `json:"role"`
	Active   *bool   `json:"active"`
}

type resetRequest struct {
	Password string `json:"password" binding:"required"`
}

type changeRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// rosterResponse adds active to what /me answers: the admin manages the inactive ones too. The hash never leaves (ADR-0030).
type rosterResponse struct {
	operatorResponse
	Active bool `json:"active"`
}

// heldResponse tells the admin what a change left behind, so the hand-over of ADR-0056 can follow (ADR-0062).
type heldResponse struct {
	rosterResponse
	HeldConversations int `json:"held_conversations"`
}

type rosterPage struct {
	Items   []rosterResponse `json:"items"`
	Total   int              `json:"total"`
	Page    int              `json:"page"`
	PerPage int              `json:"per_page"`
}

func presentRoster(o Operator) rosterResponse {
	return rosterResponse{operatorResponse: present(o), Active: o.Active}
}

func (c *RosterController) list(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	filter, err := filterFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	page, perPage, err := pagination(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	found, err := c.roster.List(gc.Request.Context(), scope, filter, perPage, (page-1)*perPage)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	items := make([]rosterResponse, 0, len(found.Items))
	for _, o := range found.Items {
		items = append(items, presentRoster(o))
	}
	soniahttp.OK(gc, rosterPage{Items: items, Total: found.Total, Page: page, PerPage: perPage})
}

func (c *RosterController) create(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	var req createRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "name, lastname, email, password and role are required").WithCause(err))
		return
	}
	created, err := c.roster.Create(gc.Request.Context(), scope, NewOperator{
		Name: req.Name, Lastname: req.Lastname, Email: req.Email, Password: req.Password, Role: enums.OperatorRole(req.Role),
	})
	if err != nil {
		soniahttp.Fail(gc, c.log, translateRoster(err))
		return
	}
	soniahttp.Created(gc, presentRoster(created))
}

func (c *RosterController) update(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	operatorID, ok := c.idFrom(gc)
	if !ok {
		return
	}
	var req updateRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "name, lastname, email, role and active are the fields of an operator").WithCause(err))
		return
	}
	patch := Patch{Name: req.Name, Lastname: req.Lastname, Email: req.Email, Active: req.Active}
	if req.Role != nil {
		role := enums.OperatorRole(*req.Role)
		patch.Role = &role
	}
	held, err := c.roster.Update(gc.Request.Context(), scope, operatorID, patch)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateRoster(err))
		return
	}
	soniahttp.OK(gc, heldResponse{rosterResponse: presentRoster(held.Operator), HeldConversations: held.Conversations})
}

// A 200 with a body and not a 204, because what the person still holds is the answer the admin needs next (ADR-0062).
func (c *RosterController) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	operatorID, ok := c.idFrom(gc)
	if !ok {
		return
	}
	held, err := c.roster.Delete(gc.Request.Context(), scope, operatorID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translateRoster(err))
		return
	}
	soniahttp.OK(gc, gin.H{"held_conversations": held})
}

func (c *RosterController) resetPassword(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	operatorID, ok := c.idFrom(gc)
	if !ok {
		return
	}
	var req resetRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "password is required").WithCause(err))
		return
	}
	if err := c.roster.ResetPassword(gc.Request.Context(), scope, operatorID, req.Password); err != nil {
		soniahttp.Fail(gc, c.log, translateRoster(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *RosterController) changePassword(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	var req changeRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "current_password and new_password are required").WithCause(err))
		return
	}
	if err := c.roster.ChangePassword(gc.Request.Context(), scope, req.CurrentPassword, req.NewPassword); err != nil {
		soniahttp.Fail(gc, c.log, translateRoster(err))
		return
	}
	soniahttp.NoContent(gc)
}

// translateRoster turns what the roster knows into what the frontend switches on, and lets anything else fall through as a 500.
func translateRoster(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("operator_not_found", "no such operator").WithCause(err)
	case errors.Is(err, ErrDuplicateEmail):
		return soniahttp.Conflict("email_taken", "an operator with that email already exists").WithCause(err)
	case errors.Is(err, ErrSelf):
		return soniahttp.Conflict("not_on_yourself", "an admin cannot change their own role or active, delete themselves or reset their own password here").WithCause(err)
	case errors.Is(err, ErrLastAdmin):
		return soniahttp.Conflict("last_admin", "the account would be left without an active admin").WithCause(err)
	case errors.Is(err, ErrPasswordChanged):
		return soniahttp.Conflict("password_changed", "the password changed meanwhile: try again").WithCause(err)
	case errors.Is(err, ErrEmptyName):
		return soniahttp.BadRequest("invalid_name", "the name and the lastname cannot be empty").WithCause(err)
	case errors.Is(err, ErrInvalidEmail):
		return soniahttp.BadRequest("invalid_email", "the email is not an address").WithCause(err)
	case errors.Is(err, ErrInvalidRole):
		return soniahttp.BadRequest("invalid_role", "the role is operator, supervisor or admin").WithCause(err)
	case errors.Is(err, ErrPasswordTooWeak):
		return soniahttp.BadRequest("password_too_short", "the password needs at least eight characters").WithCause(err)
	case errors.Is(err, ErrPasswordTooLong):
		return soniahttp.BadRequest("password_too_long", "the password is longer than 72 bytes").WithCause(err)
	case errors.Is(err, ErrWrongCurrentPassword):
		return soniahttp.BadRequest("wrong_current_password", "the current password does not match").WithCause(err)
	default:
		return err
	}
}

func filterFrom(gc *gin.Context) (Filter, error) {
	var f Filter
	if raw := gc.Query("role"); raw != "" {
		role := enums.OperatorRole(raw)
		if !role.IsValid() {
			return Filter{}, soniahttp.BadRequest("invalid_role", "the role is operator, supervisor or admin")
		}
		f.Role = &role
	}
	if raw := gc.Query("active"); raw != "" {
		active, err := strconv.ParseBool(raw)
		if err != nil {
			return Filter{}, soniahttp.BadRequest("invalid_active", "active is true or false").WithCause(err)
		}
		f.Active = &active
	}
	return f, nil
}

func (c *RosterController) idFrom(gc *gin.Context) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the operator id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return parsed, true
}

func (c *RosterController) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID}, true
}

// A page past the end answers an empty list with the real total, as in labels/.
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
		return 0, errors.New("operators: not a positive whole number")
	}
	return n, nil
}
