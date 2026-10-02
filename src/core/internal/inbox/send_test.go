package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// TestWhoMayAnswerIsTheHolderOrThePool is ADR-0035, regla 5, as a table: the conversation is the caller's or nobody's, for
// every role. The pool is taken by whoever answers (ADR-0004); a conversation held by another is refused before anything is written.
func TestWhoMayAnswerIsTheHolderOrThePool(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name      string
		holder    *uuid.UUID
		role      enums.OperatorRole
		wantErr   error
		wantClaim bool
	}{
		{"an operator answering the pool takes it", nil, enums.OperatorRoleOperator, nil, true},
		{"a supervisor answering the pool takes it too: ADR-0004 says the first operator, whatever the role", nil, enums.OperatorRoleSupervisor, nil, true},
		{"an operator answering its own conversation claims nothing", &me, enums.OperatorRoleOperator, nil, false},
		{"an operator is told another's conversation does not exist, as the listing never showed it", &other, enums.OperatorRoleOperator, ErrNotFound, false},
		{"a supervisor sees the whole team and is told who holds it, not that it is missing", &other, enums.OperatorRoleSupervisor, ErrTaken, false},
		{"an admin is refused the same way: answering is not reassigning, and reassigning is F26", &other, enums.OperatorRoleAdmin, ErrTaken, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)

			message, err := d.service.Send(context.Background(), scopeOf(me, tc.role), target.ConversationID, "  te paso la dirección  ", nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Send: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(d.aggregate.recorded) != 0 || d.sender.calls != 0 || len(d.store.assignments) != 0 {
					t.Fatalf("a refused answer still wrote or sent: %d rows, %d sends, %d assignments",
						len(d.aggregate.recorded), d.sender.calls, len(d.store.assignments))
				}
				return
			}

			if message.Status == nil || *message.Status != enums.MessageStatusSent {
				t.Fatalf("status %v, want sent", message.Status)
			}
			if d.sender.texts[0] != "te paso la dirección" || d.sender.to[0] != target.ContactPhone {
				t.Fatalf("sent %q to %q, want the trimmed text to the contact", d.sender.texts[0], d.sender.to[0])
			}
			if _, claimed := d.aggregate.claimed[target.ConversationID]; claimed != tc.wantClaim {
				t.Fatalf("claimed = %v, want %v", claimed, tc.wantClaim)
			}
			if tc.wantClaim {
				if len(d.store.assignments) != 1 {
					t.Fatalf("%d assignments written, want the one F26 will read as history", len(d.store.assignments))
				}
				a := d.store.assignments[0]
				if a.FromOperatorID != nil || a.ToOperatorID != me || a.AssignedByOperatorID == nil || *a.AssignedByOperatorID != me {
					t.Fatalf("assignment %+v, want from nobody to me, by me (DER, entidades)", a)
				}
				if a.Reason != autoAssignReason {
					t.Fatalf("reason %q, want %q", a.Reason, autoAssignReason)
				}
			} else if len(d.store.assignments) != 0 {
				t.Fatalf("an answer on a held conversation wrote an assignment: %+v", d.store.assignments)
			}
		})
	}
}

// TestLosingThePoolRaceWritesNothingAndSendsNothing is why the claim sits in the transaction of the answer: the second operator's
// text never becomes a row, and above all never reaches the customer as a second answer (ADR-0004, ADR-0020).
func TestLosingThePoolRaceWritesNothingAndSendsNothing(t *testing.T) {
	d := newDesk(&stubStore{})
	d.aggregate.claimErr = conversations.ErrTaken
	target := d.open(nil)

	_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
	if !errors.Is(err, ErrTaken) {
		t.Fatalf("Send: got %v, want ErrTaken", err)
	}
	if len(d.aggregate.recorded) != 0 || d.sender.calls != 0 || len(d.store.assignments) != 0 {
		t.Fatalf("the loser wrote or sent: %d rows, %d sends, %d assignments",
			len(d.aggregate.recorded), d.sender.calls, len(d.store.assignments))
	}
}

// TestAnAnswerMetaWouldRefuseIsRefusedBeforeItIsWritten covers the refusals that come from the conversation and not from the
// caller: none of them leaves a row, because a row Meta was always going to reject is a failure nobody caused (ADR-0035, regla 9).
func TestAnAnswerMetaWouldRefuseIsRefusedBeforeItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shape   func(*Target)
		text    string
		wantErr error
	}{
		{"the 24 hours are over: only a template could reach the contact (ADR-0008)",
			func(t *Target) { t.WindowExpiresAt = time.Now().Add(-time.Minute) }, "hola", ErrWindowClosed},
		{"the conversation is resolved: the next inbound message opens a new one (ADR-0002)",
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, "hola", ErrResolved},
		{"an answer of blanks is no answer", func(*Target) {}, "   ", ErrEmptyText},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, tc.text, nil)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Send: got %v, want %v", err, tc.wantErr)
			}
			if len(d.aggregate.recorded) != 0 || d.sender.calls != 0 || len(d.aggregate.claimed) != 0 {
				t.Fatalf("a refused answer still wrote, claimed or sent")
			}
		})
	}

	d := newDesk(&stubStore{})
	if _, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), "hola", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Send to a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}

// TestAFailedSendIsTheRowSaidFailedAndNeverRetried is ADR-0020 from the operator's side: one attempt, the failure on the row and in
// the answer, and the conversation still taken, because the failure needs an owner to decide about it (ADR-0035, regla 7).
func TestAFailedSendIsTheRowSaidFailedAndNeverRetried(t *testing.T) {
	refused := &httpclient.StatusError{Status: 400, Body: `{"error":{"message":"(#131030) Recipient not in allowed list","code":131030}}`}

	for _, tc := range []struct {
		name  string
		shape func(*desk)
		// The code and not the cause: the operator reads it in the inbox, and the body Meta answered with goes to the log.
		code string
	}{
		{"Meta refuses the send", func(d *desk) { d.sender.err = refused }, "131030"},
		// Ready said yes and the send still failed: the token was revoked in between, which no check made in advance can see.
		{"the number stopped being able to send after the check", func(d *desk) { d.sender.err = whatsapp.ErrInboxNotConfigured },
			"inbox_not_configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			tc.shape(d)
			me := id.New()
			target := d.open(nil)

			message, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
			if err != nil {
				t.Fatalf("Send: got %v, want the failed row and no error", err)
			}
			if message.Status == nil || *message.Status != enums.MessageStatusFailed {
				t.Fatalf("status %v, want failed", message.Status)
			}
			if message.ErrorCode == nil || *message.ErrorCode != tc.code {
				t.Fatalf("error_code %v, want %q", message.ErrorCode, tc.code)
			}
			if d.aggregate.failed[message.ID] != tc.code {
				t.Fatalf("the row was marked %v, want %q", d.aggregate.failed, tc.code)
			}
			if d.sender.calls > 1 {
				t.Fatalf("sent %d times, want at most once", d.sender.calls)
			}
			if d.aggregate.claimed[target.ConversationID] != me {
				t.Fatalf("the failed answer left the conversation without an owner")
			}
		})
	}
}

// TestANumberThatCannotSendIsRefusedBeforeWriting is ADR-0035, regla 9, for the number: a failure known in advance is refused like
// the closed window, with no row, no claim and no send, instead of leaving the operator a failed answer it did not cause.
func TestANumberThatCannotSendIsRefusedBeforeWriting(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name  string
		shape func(*desk)
		want  error
	}{
		{"the number does not exist", func(d *desk) { d.channel.err = whatsapp.ErrInboxNotFound }, ErrChannelUnavailable},
		{"the number was never connected", func(d *desk) { d.sender.readyErr = whatsapp.ErrInboxNotConfigured }, ErrChannelUnavailable},
		// Not a refusal: the database failing says nothing about the number, so it surfaces as the failure it is.
		{"the number could not be read", func(d *desk) { d.channel.err = boom }, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			tc.shape(d)
			target := d.open(nil)

			_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Send: got %v, want %v", err, tc.want)
			}
			if len(d.aggregate.recorded) != 0 || len(d.aggregate.claimed) != 0 || d.sender.calls != 0 || len(d.store.assignments) != 0 {
				t.Fatalf("a refused answer still wrote, claimed or sent: %d rows, %d claims, %d sends, %d assignments",
					len(d.aggregate.recorded), len(d.aggregate.claimed), d.sender.calls, len(d.store.assignments))
			}
		})
	}
}

// TestADeliveredAnswerIsNeverReportedAsAnError covers the write after a successful send failing: answering an error would invite
// the operator to send it again, which is the duplicate ADR-0020 exists to prevent.
func TestADeliveredAnswerIsNeverReportedAsAnError(t *testing.T) {
	d := newDesk(&stubStore{})
	d.aggregate.markSentErr = errors.New("connection reset")
	target := d.open(nil)

	message, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
	if err != nil {
		t.Fatalf("Send: got %v, want the sent answer", err)
	}
	if message.Status == nil || *message.Status != enums.MessageStatusSent {
		t.Fatalf("status %v, want sent: the customer has it", message.Status)
	}

	// And the other side: a failure that cannot even be marked is still reported as a failure, never as sent.
	d = newDesk(&stubStore{})
	d.sender.err = errors.New("timeout")
	d.aggregate.markFailedErr = errors.New("connection reset")
	target = d.open(nil)
	message, err = d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
	if err != nil || message.Status == nil || *message.Status != enums.MessageStatusFailed {
		t.Fatalf("got %v and status %v, want failed and no error", err, message.Status)
	}
}

// TestClosingTheTabDoesNotCutTheSend is the detached context: the request is gone, and the call Meta may already have accepted
// runs to its end anyway, so the row records what happened instead of a cancellation.
func TestClosingTheTabDoesNotCutTheSend(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	message, err := d.service.Send(ctx, scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.sender.ctxErr != nil {
		t.Fatalf("the send ran on a cancelled context: %v", d.sender.ctxErr)
	}
	if message.Status == nil || *message.Status != enums.MessageStatusSent {
		t.Fatalf("status %v, want sent", message.Status)
	}
}

// TestAWriteThatFailsInsideTheTransactionSendsNothing keeps the order of 07-secuencia: without the row, nothing leaves.
func TestAWriteThatFailsInsideTheTransactionSendsNothing(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name  string
		shape func(*desk)
	}{
		{"the claim fails for a reason that is not the race", func(d *desk) { d.aggregate.claimErr = boom }},
		{"the assignment cannot be written", func(d *desk) { d.store.assignErr = boom }},
		{"the answer cannot be written", func(d *desk) { d.aggregate.recordErr = boom }},
		{"the conversation cannot be read", func(d *desk) { d.store.err = boom }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			tc.shape(d)

			_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
			if !errors.Is(err, boom) {
				t.Fatalf("Send: got %v, want the underlying failure", err)
			}
			if d.sender.calls != 0 {
				t.Fatalf("an answer with no row reached the customer")
			}
		})
	}
}

// ADR-0046, section 2.5: the text copied from a suggestion is sent as the Operator's, and the draft is reviewed with the message in
// the same transaction, as accepted when it left untouched.
func TestASentSuggestionIsReviewedWithTheMessage(t *testing.T) {
	me := id.New()
	d := newDesk(&stubStore{})
	target := d.open(&me)
	draft := id.New()

	message, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "  Sí, tenemos dos.  ", &draft)
	if err != nil {
		t.Fatalf("Send: got %v, want the answer sent", err)
	}
	if message.SenderType != enums.SenderTypeOperator {
		t.Fatalf("sender_type = %q, want operator: the Operator sent it and may have changed it", message.SenderType)
	}
	if len(d.reviews.reviewed) != 1 {
		t.Fatalf("reviews = %v, want the draft reviewed once", d.reviews.reviewed)
	}
	got := d.reviews.reviewed[0]
	if got.generationID != draft || got.messageID != message.ID || got.operatorID != me || got.text != "Sí, tenemos dos." {
		t.Fatalf("the review named %+v, want this draft, this message, this Operator and the trimmed text", got)
	}
}

// An answer written alone names no suggestion, and nothing is reviewed.
func TestAnAnswerWithNoSuggestionReviewsNothing(t *testing.T) {
	me := id.New()
	d := newDesk(&stubStore{})
	target := d.open(&me)

	if _, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(d.reviews.reviewed) != 0 {
		t.Fatalf("a draft was reviewed for an answer that named none: %v", d.reviews.reviewed)
	}
}

// A draft of another conversation, or one already sent, refuses the whole send: the answer and its review commit together.
func TestASuggestionThatIsNotPendingRefusesTheSend(t *testing.T) {
	me := id.New()
	d := newDesk(&stubStore{})
	target := d.open(&me)
	d.reviews.err = turn.ErrGenerationNotFound
	draft := id.New()

	_, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola", &draft)
	if !errors.Is(err, ErrSuggestionNotPending) {
		t.Fatalf("Send: got %v, want ErrSuggestionNotPending", err)
	}
	if d.sender.calls != 0 {
		t.Fatal("the answer left although its review was refused")
	}
}

// The draft is read with the reach of the thread: what the Operator cannot read it cannot copy (ADR-0035, regla 2).
func TestTheSuggestionIsReadWithTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()
	d := newDesk(&stubStore{})
	mine := d.open(&me)
	theirs := d.open(&other)
	d.store.suggestions = map[uuid.UUID]Suggestion{
		mine.ConversationID:   {GenerationID: id.New(), Text: "Sí, tenemos dos."},
		theirs.ConversationID: {GenerationID: id.New(), Text: "otra"},
	}

	got, err := d.service.Suggestion(context.Background(), scopeOf(me, enums.OperatorRoleOperator), mine.ConversationID)
	if err != nil || got.Text != "Sí, tenemos dos." {
		t.Fatalf("Suggestion: got %+v, %v, want the draft of my conversation", got, err)
	}
	if _, err := d.service.Suggestion(context.Background(), scopeOf(me, enums.OperatorRoleOperator), theirs.ConversationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Suggestion of another's: got %v, want ErrNotFound", err)
	}
}
