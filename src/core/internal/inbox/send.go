package inbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// Send writes the operator's answer before it leaves and sends it at most once (ADR-0020). Taking the conversation out of the pool
// happens in the same transaction as the row, so losing the race writes nothing and sends nothing (ADR-0035, reglas 7 y 8).
//
// A send that Meta refuses is not an error of this call: the row exists, it says failed, and the caller gets it back to show.
//
// generationID names the suggestion the text was copied from, and its review commits with the message or neither does (ADR-0046).
func (s *Service) Send(ctx context.Context, scope Scope, conversationID uuid.UUID, text string, generationID *uuid.UUID) (Message, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Message{}, ErrEmptyText
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return Message{}, err
	}
	if err := mayAnswer(scope, target); err != nil {
		return Message{}, err
	}
	if target.Status == enums.ConversationStatusResolved {
		return Message{}, ErrResolved
	}
	// Refused before anything is written: a row Meta was always going to reject is a failure the operator did not cause (ADR-0035, regla 9).
	now := time.Now().UTC()
	if !now.Before(target.WindowExpiresAt) {
		return Message{}, ErrWindowClosed
	}
	// The same reasoning for the number, read on the request's context so a stuck read ends with the request. A token revoked after
	// this point still fails on the send, as before: no check made in advance can see that.
	inbox, err := s.channelFor(ctx, target)
	if err != nil {
		return Message{}, err
	}

	var message conversations.Message
	err = s.opts.Tx(ctx, func(store Store, aggregate Aggregate, reviews Reviews) error {
		if target.OperatorID == nil {
			if err := claim(ctx, store, aggregate, target, scope.OperatorID); err != nil {
				return err
			}
		}
		var err error
		message, err = aggregate.RecordOperatorOutbound(ctx, target.ConversationID, scope.OperatorID, text, now)
		if err != nil || generationID == nil {
			return err
		}
		return review(ctx, reviews, *generationID, target.ConversationID, message.ID, scope.OperatorID, text)
	})
	if err != nil {
		return Message{}, err
	}

	delivered := s.deliver(ctx, target, inbox, message)
	// After the send, so the row the event carries already says sent or failed. Taking the pool is the one change that takes the
	// row out of the other operators' inboxes (ADR-0035, regla 7).
	if target.OperatorID == nil {
		s.opts.Events.LeftPool(target.ConversationID)
	} else {
		s.opts.Events.Changed(target.ConversationID)
	}
	return delivered, nil
}

// review marks the draft accepted or edited, compared with the trimmed text the Operator sent (ADR-0046, section 2.5).
func review(ctx context.Context, reviews Reviews, generationID, conversationID, messageID, operatorID uuid.UUID, text string) error {
	_, err := reviews.ReviewGeneration(ctx, generationID, conversationID, messageID, operatorID, text)
	if errors.Is(err, turn.ErrGenerationNotFound) {
		return ErrSuggestionNotPending
	}
	return err
}

// Suggestion answers the draft waiting in the conversation, with the reach of the thread: what the Operator cannot read it cannot
// copy either (ADR-0035, regla 2).
func (s *Service) Suggestion(ctx context.Context, scope Scope, conversationID uuid.UUID) (Suggestion, error) {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return Suggestion{}, err
	}
	if !mayRead(scope, target) {
		return Suggestion{}, ErrNotFound
	}
	return s.opts.Repo.PendingSuggestion(ctx, conversationID)
}

// channelFor answers the number the conversation sends from, or ErrChannelUnavailable when it is known now that no send can leave.
func (s *Service) channelFor(ctx context.Context, target Target) (whatsapp.Inbox, error) {
	inbox, err := s.opts.Channel.InboxByID(ctx, target.InboxID)
	if errors.Is(err, whatsapp.ErrInboxNotFound) {
		return whatsapp.Inbox{}, fmt.Errorf("%w: %w", ErrChannelUnavailable, err)
	}
	if err != nil {
		return whatsapp.Inbox{}, err
	}
	if err := s.opts.Sender.Ready(inbox); err != nil {
		return whatsapp.Inbox{}, fmt.Errorf("%w: %w", ErrChannelUnavailable, err)
	}
	return inbox, nil
}

// deliver runs outside the transaction, because none may span the boundary with another service (ADR-0017 (i).3).
//
// It is detached from the request: the operator pressed send, and closing the tab a moment later must not cut a call Meta may
// already have accepted. The client's own timeout bounds the send, and writeGrace bounds the write after it.
func (s *Service) deliver(ctx context.Context, target Target, inbox whatsapp.Inbox, message conversations.Message) Message {
	ctx = context.WithoutCancel(ctx)

	// No retry of any kind: a duplicate answer to a customer cannot be taken back (ADR-0020).
	wamid, err := s.opts.Sender.Send(ctx, inbox, target.ContactPhone, message.ContentText)
	if err != nil {
		return s.undelivered(ctx, message, err)
	}

	writeCtx, cancel := context.WithTimeout(ctx, writeGrace)
	defer cancel()

	sentAt := time.Now().UTC()
	status := enums.MessageStatusSent
	if err := s.opts.Aggregate.MarkSent(writeCtx, message.ID, wamid, sentAt); err != nil {
		// Delivered and not recorded. Answering an error would invite the operator to send it again, which is the duplicate ADR-0020
		// exists to prevent, so the answer says sent and the log carries the wamid the row needs to be reconciled by hand.
		s.opts.Logger.Error("operator answer delivered but not recorded",
			slog.String("message_id", message.ID.String()),
			slog.String("wamid", wamid),
			slog.String("cause", err.Error()),
		)
	}
	return asDrawn(message, &status, nil, sentAt)
}

// undelivered leaves the failure on the row instead of retrying it, and gives it back so the operator decides (ADR-0020).
func (s *Service) undelivered(ctx context.Context, message conversations.Message, cause error) Message {
	writeCtx, cancel := context.WithTimeout(ctx, writeGrace)
	defer cancel()

	code := whatsapp.FailureCode(cause)
	status := enums.MessageStatusFailed
	if err := s.opts.Aggregate.MarkFailed(writeCtx, message.ID, code); err != nil {
		// The row stays with no status, which the inbox draws as not sent: the failure is still visible, only less precise.
		s.opts.Logger.Error("operator answer failed and the row could not be marked",
			slog.String("message_id", message.ID.String()),
			slog.String("cause", fmt.Errorf("%w; mark failed: %w", cause, err).Error()),
		)
	}
	s.opts.Logger.Warn("operator answer not delivered",
		slog.String("conversation_id", message.ConversationID.String()),
		slog.String("message_id", message.ID.String()),
		slog.String("error_code", code),
		slog.String("cause", cause.Error()),
	)
	return asDrawn(message, &status, &code, message.SentAt)
}

func asDrawn(m conversations.Message, status *enums.MessageStatus, errorCode *string, sentAt time.Time) Message {
	return Message{
		ID: m.ID, Direction: m.Direction, SenderType: m.SenderType,
		ContentText: m.ContentText, ContentType: m.ContentType,
		Status: status, ErrorCode: errorCode, SentAt: sentAt,
	}
}
