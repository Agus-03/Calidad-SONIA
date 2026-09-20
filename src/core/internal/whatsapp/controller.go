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

// Where Meta puts the HMAC of the body; the service checks it, because only the inbox the payload names holds the key.
const signatureHeader = "X-Hub-Signature-256"

// Declared by the channel and implemented by turn/. The webhook no longer starts a turn: it says which batch the message joins,
// and what answers is the poller, once that batch stops waiting for more (ADR-0041, regla 7).
type MessageCollector interface {
	Collect(ctx context.Context, conversationID, messageID uuid.UUID, aiAgentID *uuid.UUID) error
}

type Controller struct {
	service   *Service
	collector MessageCollector
	log       *slog.Logger
}

// collector nil simply means the message is stored and joins no batch, which is what every test of the channel alone needs.
func NewController(service *Service, collector MessageCollector, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{service: service, collector: collector, log: log}
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

	// A forged body still gets its 200: the service keeps it as discarded and returns nothing to start (F20).
	inbound, err := c.service.Receive(gc.Request.Context(), body, gc.GetHeader(signatureHeader), time.Now().UTC())
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

	// Before the 200 and not after it: two statements against a pool that is already open, and a message that reached no batch is
	// one nobody will ever answer (ADR-0041, regla 2). What Meta is still not kept waiting for is the turn itself (ADR-0029).
	if c.collector != nil {
		for _, in := range inbound {
			if err := c.collector.Collect(gc.Request.Context(), in.ConversationID, in.MessageID, in.AIAgentID); err != nil {
				// Still a 200: the message is stored, and Meta's retry would be skipped as a redelivery and batch nothing either.
				c.log.Error("message not added to its batch",
					slog.String("message_id", in.MessageID.String()),
					slog.String("cause", err.Error()),
				)
			}
		}
	}

	soniahttp.OK(gc, gin.H{"received": len(inbound)})
}
