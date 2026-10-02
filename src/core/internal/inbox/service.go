package inbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

var (
	// ErrNotFound covers a conversation that does not exist and one this operator cannot reach: telling them apart would confirm
	// to an operator that another's conversation exists, which is what contacts/ already answers too (ADR-0035, regla 3).
	ErrNotFound = errors.New("inbox: conversation not found")

	// ErrTaken is somebody else holding the conversation: the pool race lost, or a supervisor answering a chat that is not theirs
	// (ADR-0035, reglas 6 y 8).
	ErrTaken = errors.New("inbox: the conversation is assigned to another operator")

	// ErrResolved refuses to write into a closed unit of work: a new message from the customer is what opens the next one (ADR-0002).
	ErrResolved = errors.New("inbox: the conversation is resolved")

	// ErrWindowClosed is Meta's rule and not ours: outside the 24 hours only a template may be sent, and SONIA sends none (ADR-0008).
	ErrWindowClosed = errors.New("inbox: the 24 hour window of the conversation is closed")

	ErrEmptyText = errors.New("inbox: the answer has no text")

	// ErrUnknownMode is a word that is not one of the three of F02, told apart from a real mode this phase does not offer yet.
	ErrUnknownMode = errors.New("inbox: the mode is not one of autonomous, suggestion, off")

	// ErrNoSuggestion is no draft waiting, which is how most conversations are: nothing to copy (ADR-0046, section 2.5).
	ErrNoSuggestion = errors.New("inbox: the conversation has no pending suggestion")

	// ErrSuggestionNotPending is a generation_id that is not a pending draft of this conversation: another's, or one already sent.
	// Refused with nothing written, because the answer and its review commit together (ADR-0046, section 2.5).
	ErrSuggestionNotPending = errors.New("inbox: the suggestion is not a pending one of this conversation")

	// ErrNoAIAgent refuses to turn on a bot the conversation was born without: conversations.ErrNoAIAgent, seen from here (ADR-0035, regla 12).
	ErrNoAIAgent = errors.New("inbox: the conversation has no agent to turn on")

	// ErrAIAgentUnavailable is an agent the conversation has and that cannot answer: paused or removed by the admin. Told apart from
	// ErrNoAIAgent because a pause ends and the vendor can try again then, and a conversation with no agent never will (ADR-0038).
	ErrAIAgentUnavailable = errors.New("inbox: the agent of the conversation is paused or removed")

	// ErrChannelUnavailable is a number that does not exist or that the sender can never send from: known before writing, so refused
	// before writing, like the closed window (ADR-0035, regla 9).
	ErrChannelUnavailable = errors.New("inbox: the number of the conversation cannot send")

	// ErrNotHeld is an operator switching the bot of, or closing, one it sees and does not hold: the pool (ADR-0037 regla 4, ADR-0054).
	ErrNotHeld = errors.New("inbox: the conversation is not assigned to the caller")

	// ErrTurnInProgress is a close while the bot still owes an answer: the frontend waits for it, and this is the backstop (ADR-0054).
	ErrTurnInProgress = errors.New("inbox: the bot is still answering the conversation")

	// ErrInvalidScore is a score off the scale of one to five the group chose, like stars (ADR-0055).
	ErrInvalidScore = errors.New("inbox: the score is not a whole number from 1 to 5")

	// ErrNotResolved is a score on an open conversation: F13 scores the closed ones, whose work is over (ADR-0055).
	ErrNotResolved = errors.New("inbox: the conversation is not resolved")

	// ErrNotTheVendor is a score from anybody but the vendor holding the conversation, who the group chose to score it (ADR-0055).
	ErrNotTheVendor = errors.New("inbox: only the vendor of the conversation scores it")
)

// autoAssignReason is what the system writes where a person would write why: a stable token and not a sentence, so the history of
// F26 can tell taking the pool by answering from the two assignments of the dispatcher (dispatch.go) and from the ones somebody asked for.
const autoAssignReason = "auto_assigned_on_first_reply"

// The scale of F13 the group chose, one to five like stars, which the schema checks too (ADR-0055).
const minScore, maxScore = 1, 5

// A timeout is the most likely cause of the write after a failed send, so it cannot run on the context that just expired.
const writeGrace = 5 * time.Second

// store is the pool-bound slice of the repository: the reads, which need no transaction of their own.
type store interface {
	List(ctx context.Context, scope Scope, filter Filter, after *Cursor, limit int) ([]Summary, error)
	Target(ctx context.Context, accountID, conversationID uuid.UUID) (Target, error)
	Thread(ctx context.Context, conversationID uuid.UUID, after *Cursor, limit int) ([]ThreadMessage, error)
	PendingSuggestion(ctx context.Context, conversationID uuid.UUID) (Suggestion, error)
	OperatorState(ctx context.Context, accountID, operatorID uuid.UUID) (bool, error)
	Assignments(ctx context.Context, conversationID uuid.UUID) ([]AssignmentEntry, error)
	// The links of F24 are this package's table, one statement each, so they need no transaction either (ADR-0057).
	AssignLabel(ctx context.Context, linkID, conversationID, labelID, operatorID uuid.UUID) (bool, error)
	UnassignLabel(ctx context.Context, conversationID, labelID uuid.UUID) (bool, error)
	LabelsOf(ctx context.Context, conversationID uuid.UUID) ([]Label, error)
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
	SetStarred(ctx context.Context, conversationID uuid.UUID, starred bool) error
	MarkRead(ctx context.Context, conversationID uuid.UUID) error
	Resolve(ctx context.Context, conversationID, operatorID uuid.UUID, turnBudget time.Duration) error
	Score(ctx context.Context, conversationID, operatorID uuid.UUID, score int) error
	Reassign(ctx context.Context, conversationID uuid.UUID, from *uuid.UUID, to uuid.UUID) error
	HandOver(ctx context.Context, from, to uuid.UUID) ([]uuid.UUID, error)
}

// Reviews is turn/, the one package of the core that writes generations: the review of a draft the Operator sent (ADR-0046, 2.5).
type Reviews interface {
	ReviewGeneration(
		ctx context.Context, generationID, conversationID, messageID, operatorID uuid.UUID, text string,
	) (enums.GenerationStatus, error)
}

// Channel is the one question this package asks whatsapp/ before sending: which number, with which token.
type Channel interface {
	InboxByID(ctx context.Context, inboxID uuid.UUID) (whatsapp.Inbox, error)
}

// Events is the hub as the service sees it, told after each write commits and never inside it (ADR-0051).
type Events interface {
	Changed(conversationID uuid.UUID)
	LeftPool(conversationID uuid.UUID)
	Reassigned(conversationID, formerHolder uuid.UUID)
}

// silence is what a service built without a hub tells: nothing, so a test or a tool never needs one.
type silence struct{}

func (silence) Changed(uuid.UUID)  {}
func (silence) LeftPool(uuid.UUID) {}

func (silence) Reassigned(uuid.UUID, uuid.UUID) {}

// TxFunc runs fn in one transaction with both collaborators bound to it (regla 5), and is a function so a test needs no PostgreSQL.
type TxFunc func(ctx context.Context, fn func(Store, Aggregate, Reviews) error) error

type Options struct {
	// Pool-bound: the reads, and the two writes after the send, which happen outside any transaction.
	Repo      store
	Aggregate Aggregate
	Catalogue Catalogue

	Tx      TxFunc
	Channel Channel
	Sender  whatsapp.Sender
	Events  Events
	Logger  *slog.Logger

	// The orchestrator's budget, so a close tells a turn still running from one a crash killed (ADR-0054).
	TurnBudget time.Duration
}

type Service struct{ opts Options }

func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Events == nil {
		opts.Events = silence{}
	}
	return &Service{opts: opts}
}

// List asks for one row more than it answers, because that row is the only honest way to know another page exists: a full page could
// just as well be the last one, and a cursor handed out for nothing sends the frontend to ask for an empty page (ADR-0036, regla 7).
func (s *Service) List(ctx context.Context, scope Scope, filter Filter, after *Cursor, limit int) (Page, error) {
	// The controller never sends less than one, but a page of zero would index the row before the first one below.
	if limit < 1 {
		limit = 1
	}

	rows, err := s.opts.Repo.List(ctx, scope, filter, after, limit+1)
	if err != nil {
		return Page{}, err
	}

	items, next := paginate(rows, limit, func(row Summary) Cursor { return Cursor{At: row.LastActivityAt, ID: row.ID} })
	return Page{Items: items, Next: next}, nil
}

// Thread reads one conversation newest first, with the same reach as the listing: what the operator could not see in the inbox it
// cannot read by guessing the id either, and it is told the conversation does not exist (ADR-0035, reglas 2 y 3).
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
// it read the whole team (ADR-0035, reglas 1 y 2).
func mayRead(scope Scope, t Target) bool {
	return CanSee(scope, t.OperatorID)
}

// CanSee is the reach as a function of who holds the conversation, so the hub decides who hears a change with the same rule the
// listing and the thread apply (ADR-0035, reglas 1 y 2).
func CanSee(scope Scope, holder *uuid.UUID) bool {
	return scope.seesTheWholeTeam() || holder == nil || *holder == scope.OperatorID
}

// maySwitch is ADR-0037, regla 4, asked of a conversation the caller already reaches (mayRead): the vendor answers for the bot of
// the conversations it holds, and the pool, which it can see, it is told it has to take first.
func maySwitch(scope Scope, t Target) error {
	if scope.seesTheWholeTeam() || t.OperatorID != nil {
		return nil
	}
	return ErrNotHeld
}

// mayAnswer is ADR-0035, reglas 5 y 6: the conversation is the caller's or nobody's, for every role. The supervisor sees the whole
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

// claim writes the assignment F26 will need as history, next to the column the reach reads: the two together or neither (ADR-0035, regla 7).
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
