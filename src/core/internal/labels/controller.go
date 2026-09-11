package labels

import (
	"errors"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// A page nobody asked to size, and a ceiling so per_page=100000 is not a query the caller can ask for.
	defaultPerPage = 20
	maxPerPage     = 100
)

// The frontend paints the label with this, so a free string would be text it cannot render. Not a rule of the anteproyecto: see ADR-0032.
var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

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

// The account and the operator are deliberately not fields: they come from the Actor, so nobody can label for another account.
type createRequest struct {
	Name  string `json:"name" binding:"required"`
	Color string `json:"color" binding:"required"`
}

type updateRequest struct {
	Name  string `json:"name" binding:"required"`
	Color string `json:"color" binding:"required"`
}

type labelResponse struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Color               string `json:"color"`
	CreatedByOperatorID string `json:"created_by_operator_id"`
}

// pageResponse carries the total and the page asked for, which is what a page number needs to know if there is a next one.
type pageResponse struct {
	Items   []labelResponse `json:"items"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
}

func present(l Label) labelResponse {
	return labelResponse{
		ID:                  l.ID.String(),
		Name:                l.Name,
		Color:               l.Color,
		CreatedByOperatorID: l.CreatedByOperatorID.String(),
	}
}

func (c *Controller) list(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}

	page, perPage, err := pagination(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	found, err := c.service.List(gc.Request.Context(), actor.AccountID, perPage, (page-1)*perPage)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	items := make([]labelResponse, 0, len(found.Items))
	for _, l := range found.Items {
		items = append(items, present(l))
	}
	soniahttp.OK(gc, pageResponse{Items: items, Total: found.Total, Page: page, PerPage: perPage})
}

func (c *Controller) create(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}

	var req createRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("invalid_body", "a name and a color are required").WithCause(err))
		return
	}
	if err := checkColor(req.Color); err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	label, err := c.service.Create(gc.Request.Context(), actor.AccountID, actor.OperatorID, req.Name, req.Color)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(label))
}

func (c *Controller) update(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}

	labelID, err := labelIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	var req updateRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log,
			soniahttp.BadRequest("invalid_body", "a name and a color are required").WithCause(err))
		return
	}
	if err := checkColor(req.Color); err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	label, err := c.service.Update(gc.Request.Context(), actor.AccountID, labelID, req.Name, req.Color)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(label))
}

func (c *Controller) remove(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}

	labelID, err := labelIDFrom(gc)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}

	if err := c.service.Delete(gc.Request.Context(), actor.AccountID, labelID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	switch {
	case errors.Is(err, ErrDuplicateName):
		return soniahttp.Conflict("label_name_taken", "a label with that name already exists").WithCause(err)
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("label_not_found", "no such label").WithCause(err)
	case errors.Is(err, ErrEmptyName):
		return soniahttp.BadRequest("invalid_name", "the name cannot be empty").WithCause(err)
	default:
		return err
	}
}

func checkColor(color string) error {
	if !hexColor.MatchString(color) {
		return soniahttp.BadRequest("invalid_color", "the color must be a hex value like #1a2b3c")
	}
	return nil
}

func labelIDFrom(gc *gin.Context) (uuid.UUID, error) {
	labelID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		return uuid.Nil, soniahttp.BadRequest("invalid_id", "the label id is not a uuid").WithCause(err)
	}
	return labelID, nil
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
		return 0, errors.New("labels: not a positive whole number")
	}
	return n, nil
}
