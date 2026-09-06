package whatsapp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Meta's payloads are small; the bound is here so a service answering badly cannot fill the core's memory.
const maxPayloadBytes = 1 << 20

// TurnStarter is declared by the channel and implemented by turn/, because the webhook is the side that must not wait for it.
type TurnStarter interface {
	Start(ctx context.Context, conversationID, messageID uuid.UUID, aiAgentID *uuid.UUID)
}

type Controller struct {
	service *Service
	starter TurnStarter
	log     *slog.Logger
}

// starter is nil until turn/ exists, and a nil one simply means the message is stored and nobody answers it yet.
func NewController(service *Service, starter TurnStarter, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{service: service, starter: starter, log: log}
}

// verify echoes the challenge verbatim: Meta compares the body byte for byte, so a JSON envelope fails the subscription.
func (c *Controller) verify(gc *gin.Context) {
	query := gc.Request.URL.Query()

	if err := c.service.Verify(gc.Request.Context(), query.Get("hub.mode"), query.Get("hub.verify_token")); err != nil {
		if errors.Is(err, ErrVerificationRejected) {
			soniahttp.Fail(gc, c.log, soniahttp.Forbidden("verification_rejected", "the verification token matches no inbox"))
			return
		}
		soniahttp.Fail(gc, c.log, soniahttp.Internal(err))
		return
	}

	soniahttp.Text(gc, query.Get("hub.challenge"))
}

// receive stores before answering, because a 200 that precedes the write would let a crash lose a message Meta will not resend.
func (c *Controller) receive(gc *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(gc.Request.Body, maxPayloadBytes))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("unreadable_payload", "the request body could not be read").WithCause(err))
		return
	}

	inbound, err := c.service.Receive(gc.Request.Context(), body, time.Now().UTC())
	switch {
	case errors.Is(err, ErrMalformedPayload):
		// A 4xx and not a 5xx: Meta retries what fails, and the same bytes will not parse the second time either.
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("malformed_payload", "the body is not a meta webhook payload"))
		return
	case errors.Is(err, ErrInboxNotFound):
		soniahttp.Fail(gc, c.log, soniahttp.NotFound("inbox_not_found", "no inbox matches that phone number id"))
		return
	case err != nil:
		soniahttp.Fail(gc, c.log, soniahttp.Internal(err))
		return
	}

	soniahttp.OK(gc, gin.H{"received": len(inbound)})

	// The turn takes seconds and Meta retries anything slower, so it runs outside the request and with a context that outlives it.
	if c.starter != nil {
		for _, in := range inbound {
			go c.starter.Start(context.WithoutCancel(gc.Request.Context()), in.ConversationID, in.MessageID, in.AIAgentID)
		}
	}
}
