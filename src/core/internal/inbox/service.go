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
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

var (
	// ErrNotFound covers a conversation that does not exist and one this operator cannot reach: telling them apart would confirm
	// to an operator that another's conversation exists, which is what contacts/ already answers too (ADR-0035, regla 3).
	ErrNotFound = errors.New("inbox: conversation not found")

	// ErrGenerationNotFound is a generation that does not exist or is not of the conversation in the path: F05 reads one answer of
	// one thread, never an id guessed from another (ADR-0049).
	ErrGenerationNotFound = errors.New("inbox: generation not found")

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
	Reasoning(ctx context.Context, conversationID, generationID uuid.UUID) (Reasoning, error)
	OperatorState(ctx context.Context, accountID, operatorID uuid.UUID) (bool, error)
	Assignments(ctx context.Context, conversationID uuid.UUID) ([]AssignmentEntry, error)
	Notes(ctx context.Context, conversationID uuid.UUID) ([]Note, error)
	NoteByID(ctx context.Context, conversationID, noteID uuid.UUID) (Note, error)
	// The notes are this package's own table and one statement each, so they need no transaction either (ADR-0056).
	AddNote(ctx context.Context, n Note) error
	EditNote(ctx context.Context, noteID, operatorID uuid.UUID, content string) error
	DeleteNote(ctx context.Context, noteID, operatorID uuid.UUID) error
	// The links of F24 are this package's table too, one statement each (ADR-0057).
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
	return canSee(scope, t.OperatorID)
}

// canSee is the reach as a function of who holds the conversation, so the hub decides who hears a change with the same rule the
// listing and the thread apply (ADR-0035, reglas 1 y 2).
func canSee(scope Scope, holder *uuid.UUID) bool {
	return scope.seesTheWholeTeam() || holder == nil || *holder == scope.OperatorID
}

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

// SetMode turns the agent of one conversation on or off without touching the others (F27, brought forward from Fase 10). The operator
// switches the conversations it holds and supervisor and admin any of the account (ADR-0037, regla 4); besides that, what this decides
// is which changes make sense for the conversation (ADR-0035, reglas 11 a 14).
//
// It does not take the conversation out of the pool: answering is what assigns, and turning the bot off is not an answer (ADR-0035, regla 13).
//
// The orchestrator reads the mode when a turn starts, so an answer the brain is already generating still goes out after the bot is
// turned off. The gap is a declared cost (ADR-0035 §4).
func (s *Service) SetMode(ctx context.Context, scope Scope, conversationID uuid.UUID, mode enums.AgentMode) (enums.AgentMode, error) {
	// All three of F02 since ADR-0046 amended regla 11 of ADR-0035: suggestion has its turn and its draft now.
	if !mode.IsValid() {
		return "", ErrUnknownMode
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return "", err
	}
	// Reach first, so a closed conversation of another vendor is still one that does not exist; then closed before not held, because
	// telling the vendor to take a resolved one first sends it to answer and be refused again (ADR-0035, regla 14).
	if !mayRead(scope, target) {
		return "", ErrNotFound
	}
	if target.Status == enums.ConversationStatusResolved {
		return "", ErrResolved
	}
	if err := maySwitch(scope, target); err != nil {
		return "", err
	}
	// Asking for the mode it already has is the state the caller wants, so it is answered and not written twice.
	if target.Mode == mode {
		return mode, nil
	}
	if mode != enums.AgentModeOff && target.AIAgentID == nil {
		return "", ErrNoAIAgent
	}
	// The same conditions the turn reads before answering: a switch that says the bot is on while the turn skips it lies to the
	// vendor, who then leaves the customer unanswered (ADR-0038).
	if mode != enums.AgentModeOff && !target.AIAgentAvailable {
		return "", ErrAIAgentUnavailable
	}

	switch err := s.opts.Aggregate.SetMode(ctx, conversationID, mode); {
	case err == nil:
		s.opts.Events.Changed(conversationID)
		return mode, nil
	case errors.Is(err, conversations.ErrNoAIAgent):
		// The read already refused a conversation with no agent, so the predicate refused a pause or a removal that committed in
		// between (ADR-0038).
		return "", ErrAIAgentUnavailable
	case errors.Is(err, conversations.ErrNotFound):
		return "", ErrNotFound
	default:
		return "", err
	}
}

// SetStarred marks one conversation for follow-up or clears the mark (F25). The reach is the thread's: whoever can read the
// conversation can star it, because F25 names no role and the star is a mark on what the caller already sees (ADR-0035, regla 2).
//
// A resolved conversation is starred too: a follow-up is what comes after the work, not part of it. And starring never assigns,
// for the same reason turning the bot off does not (ADR-0035, regla 13).
func (s *Service) SetStarred(ctx context.Context, scope Scope, conversationID uuid.UUID, starred bool) (bool, error) {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return false, err
	}
	if !mayRead(scope, target) {
		return false, ErrNotFound
	}
	// The same as the mode: asking for the state it already has is answered and not written twice.
	if target.Starred == starred {
		return starred, nil
	}

	if err := s.opts.Aggregate.SetStarred(ctx, conversationID, starred); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return false, ErrNotFound
		}
		return false, err
	}
	s.opts.Events.Changed(conversationID)
	return starred, nil
}

// MarkRead clears the unread count when the conversation is on screen (ADR-0052). Who may clear it is who may answer it: its holder,
// or anybody when it is in the pool (ADR-0035, regla 5). A supervisor looking at a vendor's conversation does not take the vendor's
// warning away, and is told the conversation is somebody else's; an operator is told it does not exist, as always.
//
// A conversation with nothing unread is answered and not written, and nobody is told: nothing changed.
func (s *Service) MarkRead(ctx context.Context, scope Scope, conversationID uuid.UUID) error {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	if err := mayAnswer(scope, target); err != nil {
		return err
	}
	if target.UnreadCount == 0 {
		return nil
	}

	if err := s.opts.Aggregate.MarkRead(ctx, conversationID); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	s.opts.Events.Changed(conversationID)
	return nil
}

// Resolve closes the conversation by hand (ADR-0002 (d)) with the reach of the switch of the bot: the vendor its own, the two roles above any (ADR-0054).
func (s *Service) Resolve(ctx context.Context, scope Scope, conversationID uuid.UUID) error {
	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return err
	}
	// The order of SetMode: another vendor's does not exist, a closed one is told so before telling the vendor to take it first.
	if !mayRead(scope, target) {
		return ErrNotFound
	}
	if target.Status == enums.ConversationStatusResolved {
		return ErrResolved
	}
	if err := maySwitch(scope, target); err != nil {
		return err
	}

	switch err := s.opts.Aggregate.Resolve(ctx, conversationID, scope.OperatorID, s.opts.TurnBudget); {
	case err == nil:
		s.opts.Events.Changed(conversationID)
		return nil
	case errors.Is(err, conversations.ErrResolved):
		return ErrResolved
	case errors.Is(err, conversations.ErrTurnInProgress):
		return ErrTurnInProgress
	default:
		return err
	}
}

// Score is F13: the vendor scores the conversation it held once it is resolved, and a new score overwrites the last (ADR-0055).
func (s *Service) Score(ctx context.Context, scope Scope, conversationID uuid.UUID, score int) (int, error) {
	if score < minScore || score > maxScore {
		return 0, ErrInvalidScore
	}

	target, err := s.opts.Repo.Target(ctx, scope.AccountID, conversationID)
	if err != nil {
		return 0, err
	}
	// Reach first, as everywhere: another vendor's conversation does not exist for an operator (ADR-0035, regla 3).
	if !mayRead(scope, target) {
		return 0, ErrNotFound
	}
	if target.Status != enums.ConversationStatusResolved {
		return 0, ErrNotResolved
	}
	if target.OperatorID == nil || *target.OperatorID != scope.OperatorID {
		return 0, ErrNotTheVendor
	}

	if err := s.opts.Aggregate.Score(ctx, conversationID, scope.OperatorID, score); err != nil {
		if errors.Is(err, conversations.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	s.opts.Events.Changed(conversationID)
	return score, nil
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
