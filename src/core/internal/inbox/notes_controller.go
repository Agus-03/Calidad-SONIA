package inbox

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

type noteRequest struct {
	Content string `json:"content"`
}

type noteResponse struct {
	ID        string           `json:"id"`
	Author    operatorResponse `json:"author"`
	Content   string           `json:"content"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
}

type notesResponse struct {
	Items []noteResponse `json:"items"`
}

func presentNote(n Note) noteResponse {
	return noteResponse{
		ID: n.ID.String(), Author: *person(&n.Author), Content: n.Content,
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
