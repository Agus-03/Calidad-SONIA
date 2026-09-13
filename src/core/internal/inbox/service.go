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
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

var (
	// ErrNotFound covers a conversation that does not exist and one this operator cannot reach: telling them apart would confirm
	// to an operator that another's conversation exists, the same reasoning contacts/ follows (ADR-0032, regla 2).
	ErrNotFound = errors.New("inbox: conversation not found")

	// ErrTaken is somebody else holding the conversation: the pool race lost, or a supervisor answering a chat that is not theirs.
	ErrTaken = errors.New("inbox: the conversation is assigned to another operator")

	// ErrResolved refuses to write into a closed unit of work: a new message from the customer is what opens the next one (ADR-0002).
	ErrResolved = errors.New("inbox: the conversation is resolved")

	// ErrWindowClosed is Meta's rule and not ours: outside the 24 hours only a template may be sent, and SONIA sends none (ADR-0008).
	ErrWindowClosed = errors.New("inbox: the 24 hour window of the conversation is closed")

	ErrEmptyText = errors.New("inbox: the answer has no text")

	// ErrUnknownMode is a word that is not one of the three of F02, told apart from a real mode this phase does not offer yet.
	ErrUnknownMode = errors.New("inbox: the mode is not one of autonomous, suggestion, off")

	// ErrModeNotAvailable is suggestion before Fase 9: accepting it would store a mode the orchestrator reads as silence, since the
	// review cycle that makes it a mode does not exist yet.
	ErrModeNotAvailable = errors.New("inbox: only autonomous and off are available until the suggestion cycle exists")

	// ErrNoAgent refuses to turn on a bot the conversation was born without (conversations.ErrNoAgent, seen from here).
	ErrNoAgent = errors.New("inbox: the conversation has no agent to turn on")
)

// autoAssignReason is what the system writes where a person would write why: a stable token and not a sentence, so the history of
// F26 can tell the automatic assignment from the ones somebody asked for.
const autoAssignReason = "auto_assigned_on_first_reply"

// A timeout is the most likely cause of the write after a failed send, so it cannot run on the context that just expired.
const writeGrace = 5 * time.Second

// store is the pool-bound slice of the repository: the reads, which need no transaction of their own.
type store interface {
	List(ctx context.Context, scope Scope, after *Cursor, limit int) ([]Summary, error)
	Target(ctx context.Context, accountID, conversationID uuid.UUID) (Target, error)
	Thread(ctx context.Context, conversationID uuid.UUID, after *Cursor, limit int) ([]ThreadMessage, error)
}

// Store is what this package writes inside the transaction it conducts. Exported because internal/app/deps.go binds it to that
// transaction from outside, the same way turn.Store and whatsapp.Store are (regla 5).
type Store interface {
	RecordAssignment(ctx context.Context, a Assignment) error
}

// Aggregate is conversations/: the inbox never writes conversations or messages with SQL of its own (regla 1).
type Aggregate interface {
	Claim(ctx context.Context, conversationID, operatorID uuid.UUID) error
	RecordOperatorOutbound(ctx context.Context, conversationID, operatorID uuid.UUID, text string, at time.Time) (conversations.Message, error)
	MarkSent(ctx context.Context, messageID uuid.UUID, wamid string, at time.Time) error
	MarkFailed(ctx context.Context, messageID uuid.UUID, errorCode string) error
	SetMode(ctx context.Context, conversationID uuid.UUID, mode enums.AgentMode) error
}

// Channel is the one question this package asks whatsapp/ before sending: which number, with which token.
type Channel interface {
	InboxByID(ctx context.Context, inboxID uuid.UUID) (whatsapp.Inbox, error)
}

// TxFunc runs fn in one transaction with both collaborators bound to it (regla 5), and is a function so a test needs no PostgreSQL.
type TxFunc func(ctx context.Context, fn func(Store, Aggregate) error) error

type Options struct {
	// Pool-bound: the reads, and the two writes after the send, which happen outside any transaction.
	Repo      store
	Aggregate Aggregate

	Tx      TxFunc
	Channel Channel
	Sender  whatsapp.Sender
	Logger  *slog.Logger
}

type Service struct{ opts Options }

func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{opts: opts}
}

// List asks for one row more than it answers, because that row is the only honest way to know another page exists: a full page could
// just as well be the last one, and a cursor handed out for nothing sends the frontend to ask for an empty page.
func (s *Service) List(ctx context.Context, scope Scope, after *Cursor, limit int) (Page, error) {
	// The controller never sends less than one, but a page of zero would index the row before the first one below.
	if limit < 1 {
		limit = 1
	}

	rows, err := s.opts.Repo.List(ctx, scope, after, limit+1)
	if err != nil {
		return Page{}, err
	}

	items, next := paginate(rows, limit, func(row Summary) Cursor { return Cursor{At: row.LastActivityAt, ID: row.ID} })
	return Page{Items: items, Next: next}, nil
}

// Thread reads one conversation newest first, with the same reach as the listing: what the operator could not see in the inbox it
// cannot read by guessing the id either, and it is told the conversation does not exist (ADR-0032, regla 2).
func (s *Service) Thread(ctx context.Context, scope Scope, conversationID uuid.UUID, after *Cursor, limit int) (ThreadPage, error) {
	if limit < 1 {
		limit = 1
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return ThreadPage{}, err
	}
	if !mayRead(scope, target) {
		return ThreadPage{}, ErrNotFound
	}

	rows, err := s.opts.Repo.Thread(ctx, conversationID, after, limit+1)
	if err != nil {
		return ThreadPage{}, err
	}

	items, next := paginate(rows, limit, func(m ThreadMessage) Cursor { return Cursor{At: m.CreatedAt, ID: m.ID} })
	return ThreadPage{Items: items, Next: next}, nil
}

// paginate takes the one row read past the limit away and names the last row shown as the cursor: the same rule for the inbox and
// for a thread, so the two can never disagree on when a page is the last one.
func paginate[T any](rows []T, limit int, cursorOf func(T) Cursor) ([]T, *Cursor) {
	if len(rows) <= limit {
		return rows, nil
	}
	rows = rows[:limit]
	next := cursorOf(rows[limit-1])
	return rows, &next
}

// mayRead is the reach of the listing applied to one conversation: the operator reads its own and the pool, and the two roles above
// it read the whole team (Anexo B).
func mayRead(scope Scope, t Target) bool {
	return scope.seesTheWholeTeam() || t.OperatorID == nil || *t.OperatorID == scope.OperatorID
}

// Send writes the operator's answer before it leaves and sends it at most once (ADR-0020). Taking the conversation out of the pool
// happens in the same transaction as the row, so losing the race writes nothing and sends nothing (ADR-0004).
//
// A send that Meta refuses is not an error of this call: the row exists, it says failed, and the caller gets it back to show.
func (s *Service) Send(ctx context.Context, scope Scope, conversationID uuid.UUID, text string) (Message, error) {
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
	// Refused before anything is written: a row Meta was always going to reject is a failure the operator did not cause.
	now := time.Now().UTC()
	if !now.Before(target.WindowExpiresAt) {
		return Message{}, ErrWindowClosed
	}

	var message conversations.Message
	err = s.opts.Tx(ctx, func(store Store, aggregate Aggregate) error {
		if target.OperatorID == nil {
			if err := claim(ctx, store, aggregate, target, scope.OperatorID); err != nil {
				return err
			}
		}
		var err error
		message, err = aggregate.RecordOperatorOutbound(ctx, target.ConversationID, scope.OperatorID, text, now)
		return err
	})
	if err != nil {
		return Message{}, err
	}

	return s.deliver(ctx, target, message), nil
}

// SetMode turns the agent of one conversation on or off without touching the others (F27, brought forward from Fase 10). Who may call it
// is decided in routes.go and not here; what this decides is which changes make sense for the conversation.
//
// It does not take the conversation out of the pool: answering is what assigns (ADR-0004), and turning the bot off is not an answer.
//
// The orchestrator reads the mode when a turn starts, so an answer the brain is already generating still goes out after the bot is
// turned off. That gap is known and is closed in Fase 9, when the three modes branch the orchestrator.
func (s *Service) SetMode(ctx context.Context, scope Scope, conversationID uuid.UUID, mode enums.AgentMode) (enums.AgentMode, error) {
	if !mode.IsValid() {
		return "", ErrUnknownMode
	}
	if mode == enums.AgentModeSuggestion {
		return "", ErrModeNotAvailable
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return "", err
	}
	if target.Status == enums.ConversationStatusResolved {
		return "", ErrResolved
	}
	// Asking for the mode it already has is the state the caller wants, so it is answered and not written twice.
	if target.Mode == mode {
		return mode, nil
	}
	if mode != enums.AgentModeOff && target.AIAgentID == nil {
		return "", ErrNoAgent
	}

	switch err := s.opts.Aggregate.SetMode(ctx, conversationID, mode); {
	case err == nil:
		return mode, nil
	case errors.Is(err, conversations.ErrNoAgent):
		return "", ErrNoAgent
	case errors.Is(err, conversations.ErrNotFound):
		return "", ErrNotFound
	default:
		return "", err
	}
}

// mayAnswer is ADR-0035, reglas 5 and 6: the conversation is the caller's or nobody's, for every role. The supervisor sees the whole
// team (Anexo B) and is told who holds a conversation; the operator cannot see it, and is told it does not exist.
func mayAnswer(scope Scope, t Target) error {
	if t.OperatorID == nil || *t.OperatorID == scope.OperatorID {
		return nil
	}
	if scope.seesTheWholeTeam() {
		return ErrTaken
	}
	return ErrNotFound
}

// claim writes the assignment F26 will need as history, next to the column the reach reads: the two together or neither.
func claim(ctx context.Context, store Store, aggregate Aggregate, target Target, operatorID uuid.UUID) error {
	if err := aggregate.Claim(ctx, target.ConversationID, operatorID); err != nil {
		if errors.Is(err, conversations.ErrTaken) {
			return ErrTaken
		}
		return err
	}
	return store.RecordAssignment(ctx, Assignment{
		ID:                   id.New(),
		ConversationID:       target.ConversationID,
		ToOperatorID:         operatorID,
		AssignedByOperatorID: &operatorID,
		Reason:               autoAssignReason,
	})
}

// deliver runs outside the transaction, because none may span the boundary with another service (ADR-0017 (i).3).
//
// It is detached from the request: the operator pressed send, and closing the tab a moment later must not cut a call Meta may
// already have accepted. The client's own timeout bounds the send, and writeGrace bounds the write after it.
func (s *Service) deliver(ctx context.Context, target Target, message conversations.Message) Message {
	ctx = context.WithoutCancel(ctx)

	inbox, err := s.opts.Channel.InboxByID(ctx, target.InboxID)
	if err != nil {
		return s.undelivered(ctx, message, err)
	}

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
