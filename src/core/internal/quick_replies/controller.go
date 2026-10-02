package quickreplies

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

const (
	// A page nobody asked to size, and a ceiling so per_page=100000 is not a query the caller can ask for, as in labels/.
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

// The account and the author are not fields: they come from the Actor, so nobody loads a reply for another account.
type createRequest struct {
	Title   string `json:"title" binding:"required"`
	Content string `json:"content" binding:"required"`
}

// Pointers so a field left out is told apart from one sent empty: pausing a reply sends only active.
type updateRequest struct {
	Title   *string `json:"title"`
	Content *string `json:"content"`
	Active  *bool   `json:"active"`
}

type replyResponse struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Content             string `json:"content"`
	Active              bool   `json:"active"`
	CreatedByOperatorID string `json:"created_by_operator_id"`
}

type pageResponse struct {
	Items   []replyResponse `json:"items"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
}

func present(q QuickReply) replyResponse {
	return replyResponse{
		ID: q.ID.String(), Title: q.Title, Content: q.Content, Active: q.Active,
		CreatedByOperatorID: q.CreatedByOperatorID.String(),
	}
}

// q searches the title and the content, which is how an operator finds the reply while typing in the chat (F09).
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
	found, err := c.service.List(gc.Request.Context(), scope, gc.Query("q"), perPage, (page-1)*perPage)
	if err != nil {
		soniahttp.Fail(gc, c.log, err)
		return
	}
	items := make([]replyResponse, 0, len(found.Items))
	for _, q := range found.Items {
		items = append(items, present(q))
	}
	soniahttp.OK(gc, pageResponse{Items: items, Total: found.Total, Page: page, PerPage: perPage})
}

func (c *Controller) create(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	var req createRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "a title and a content are required").WithCause(err))
		return
	}
	reply, err := c.service.Create(gc.Request.Context(), scope, req.Title, req.Content)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(reply))
}

func (c *Controller) update(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	replyID, ok := c.idFrom(gc)
	if !ok {
		return
	}
	var req updateRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "title, content and active are the fields of a quick reply").WithCause(err))
		return
	}
	reply, err := c.service.Update(gc.Request.Context(), scope, replyID, req.Title, req.Content, req.Active)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, present(reply))
}

func (c *Controller) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	replyID, ok := c.idFrom(gc)
	if !ok {
		return
	}
	if err := c.service.Delete(gc.Request.Context(), scope, replyID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return soniahttp.NotFound("quick_reply_not_found", "no such quick reply").WithCause(err)
	case errors.Is(err, ErrDuplicateTitle):
		return soniahttp.Conflict("quick_reply_title_taken", "a quick reply with that title already exists").WithCause(err)
	case errors.Is(err, ErrEmptyTitle):
		return soniahttp.BadRequest("invalid_title", "the title cannot be empty").WithCause(err)
	case errors.Is(err, ErrEmptyContent):
		return soniahttp.BadRequest("invalid_content", "the content cannot be empty").WithCause(err)
	default:
		return err
	}
}

func (c *Controller) idFrom(gc *gin.Context) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_id", "the quick reply id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return parsed, true
}

func (c *Controller) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role}, true
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
		return 0, errors.New("quick_replies: not a positive whole number")
	}
	return n, nil
}
