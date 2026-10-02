package notes

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
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

type noteRequest struct {
	Content string `json:"content"`
}

// The same three fields the inbox names a person with, so the frontend draws an author as it draws a vendor.
type authorResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Lastname string `json:"lastname"`
}

type noteResponse struct {
	ID        string         `json:"id"`
	Author    authorResponse `json:"author"`
	Content   string         `json:"content"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type notesResponse struct {
	Items []noteResponse `json:"items"`
}

func presentNote(n Note) noteResponse {
	return noteResponse{
		ID: n.ID.String(), Author: authorResponse{ID: n.Author.ID.String(), Name: n.Author.Name, Lastname: n.Author.Lastname}, Content: n.Content,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// notes has no cursor, like the assignments: a conversation gathers a handful of notes, not pages of them (ADR-0056).
func (c *Controller) notes(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}

	notes, err := c.service.Notes(gc.Request.Context(), scope, conversationID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	out := notesResponse{Items: make([]noteResponse, 0, len(notes))}
	for _, n := range notes {
		out.Items = append(out.Items, presentNote(n))
	}
	soniahttp.OK(gc, out)
}

func (c *Controller) addNote(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return
	}
	content, ok := c.noteContentFrom(gc)
	if !ok {
		return
	}

	note, err := c.service.AddNote(gc.Request.Context(), scope, conversationID, content)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, presentNote(note))
}

func (c *Controller) editNote(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, noteID, ok := c.noteIDsFrom(gc)
	if !ok {
		return
	}
	content, ok := c.noteContentFrom(gc)
	if !ok {
		return
	}

	note, err := c.service.EditNote(gc.Request.Context(), scope, conversationID, noteID, content)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.OK(gc, presentNote(note))
}

func (c *Controller) deleteNote(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	conversationID, noteID, ok := c.noteIDsFrom(gc)
	if !ok {
		return
	}

	if err := c.service.DeleteNote(gc.Request.Context(), scope, conversationID, noteID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

func (c *Controller) noteIDsFrom(gc *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	conversationID, ok := c.conversationIDFrom(gc)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	noteID, err := uuid.Parse(gc.Param("note_id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_note_id", "the note id is not a uuid").WithCause(err))
		return uuid.Nil, uuid.Nil, false
	}
	return conversationID, noteID, true
}

// An empty text is left to the service, which refuses it with its own code; only a body that is not JSON is refused here.
func (c *Controller) noteContentFrom(gc *gin.Context) (string, bool) {
	var req noteRequest
	if err := gc.ShouldBindJSON(&req); err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_body", "content is required").WithCause(err))
		return "", false
	}
	return req.Content, true
}

// The codes are the ones the inbox answered with before the package was apart, so the frontend switches on the same words (ADR-0063).
func translate(err error) error {
	switch {
	case errors.Is(err, inbox.ErrNotFound):
		return soniahttp.NotFound("conversation_not_found", "no such conversation").WithCause(err)
	case errors.Is(err, ErrEmptyNote):
		return soniahttp.BadRequest("invalid_content", "the note cannot be empty").WithCause(err)
	case errors.Is(err, ErrNoteNotFound):
		return soniahttp.NotFound("note_not_found", "no such note in this conversation").WithCause(err)
	case errors.Is(err, ErrNotTheAuthor):
		return soniahttp.Forbidden("not_the_author", "only the author edits or deletes a note").WithCause(err)
	default:
		return err
	}
}

func (c *Controller) scopeFrom(gc *gin.Context) (inbox.Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return inbox.Scope{}, false
	}
	return inbox.Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role}, true
}

func (c *Controller) conversationIDFrom(gc *gin.Context) (uuid.UUID, bool) {
	conversationID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_conversation_id", "the conversation id is not a uuid").WithCause(err))
		return uuid.Nil, false
	}
	return conversationID, true
}
