package operators

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// LoginController is separate from Controller because Routes returns one list and the router mounts each list in its own zone.
type LoginController struct {
	service *Service
	log     *slog.Logger
}

func NewLoginController(service *Service, log *slog.Logger) *LoginController {
	if log == nil {
		log = slog.Default()
	}
	return &LoginController{service: service, log: log}
}

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

// Bound by gin, so a missing or malformed field is a 400 before the service runs and before bcrypt costs anything.
type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// operatorResponse names the five fields that leave the process, and PasswordHash of Operator is deliberately not one (ADR-0030).
type operatorResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Lastname string `json:"lastname"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

type loginResponse struct {
	Token     string           `json:"token"`
	ExpiresAt time.Time        `json:"expires_at"`
	Operator  operatorResponse `json:"operator"`
}

func present(o Operator) operatorResponse {
	return operatorResponse{
		ID:       o.ID.String(),
		Name:     o.Name,
		Lastname: o.Lastname,
		Email:    o.Email,
		Role:     string(o.Role),
	}
}

// login answers one 401 for the three ways it can fail, because which one it was is written to the log and not to the caller.
func (c *LoginController) login(gc *gin.Context) {
	var req loginRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("invalid_body", "an email and a password are required").WithCause(err))
		return
	}

	session, err := c.service.Login(gc.Request.Context(), req.Email, req.Password, time.Now().UTC())
	if errors.Is(err, ErrInvalidCredentials) {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("invalid_credentials", "the email or the password is wrong"))
		return
	}
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	// The expiry travels so the frontend can see the session end without decoding a token it should treat as opaque.
	soniahttp.OK(gc, loginResponse{
		Token:     session.Token,
		ExpiresAt: session.ExpiresAt,
		Operator:  present(session.Operator),
	})
}

// me reads the row again instead of carrying the name and the email in the request context, which would grow the seam for one screen.
func (c *Controller) me(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}

	operator, err := c.service.Resolve(gc.Request.Context(), actor.OperatorID)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInactiveOperator) {
		soniahttp.Fail(gc, c.log,
			soniahttp.Unauthorized("session_no_longer_valid", "this session no longer belongs to an active operator").WithCause(err))
		return
	}
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	soniahttp.OK(gc, present(operator))
}
