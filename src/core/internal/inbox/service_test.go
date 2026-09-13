package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rowsAt builds n conversations one minute apart, newest first, all in the pool so every role reaches them.
func rowsAt(n int) []Summary {
	start := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	rows := make([]Summary, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, Summary{ID: id.New(), LastActivityAt: start.Add(-time.Duration(i) * time.Minute)})
	}
	return rows
}

var adminScope = Scope{AccountID: id.New(), OperatorID: id.New(), Role: enums.OperatorRoleAdmin}

// TestTheNextCursorIsHandedOutOnlyWhenAnotherPageExists protects the one row read past the page: without it a full last page
// hands out a cursor to nothing, and the frontend asks for an empty page to learn that it is done.
func TestTheNextCursorIsHandedOutOnlyWhenAnotherPageExists(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   int
		limit    int
		wantRows int
		wantNext bool
	}{
		{"more rows than the page holds: the cursor names the last row shown", 5, 2, 2, true},
		{"exactly a page: the extra row is not there, so there is no next page", 2, 2, 2, false},
		{"less than a page: no next page", 1, 2, 1, false},
		{"nothing at all: an empty page and no cursor", 0, 2, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubStore{rows: rowsAt(tc.stored)}

			page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, nil, tc.limit)
			if err != nil {
				t.Fatalf("List: got %v, want no error", err)
			}
			if store.lastLimit != tc.limit+1 {
				t.Fatalf("the store was asked for %d rows, want %d: one past the page", store.lastLimit, tc.limit+1)
			}
			if len(page.Items) != tc.wantRows {
				t.Fatalf("the page holds %d rows, want %d", len(page.Items), tc.wantRows)
			}
			if (page.Next != nil) != tc.wantNext {
				t.Fatalf("next cursor present = %v, want %v", page.Next != nil, tc.wantNext)
			}
			if tc.wantNext {
				last := page.Items[len(page.Items)-1]
				if page.Next.ID != last.ID || !page.Next.LastActivityAt.Equal(last.LastActivityAt) {
					t.Fatalf("the cursor names %+v, want the last row shown, %v", page.Next, last.ID)
				}
			}
		})
	}
}

// TestAPageOfZeroIsReadAsAPageOfOne keeps a caller that forgot the limit from indexing the row before the first one.
func TestAPageOfZeroIsReadAsAPageOfOne(t *testing.T) {
	store := &stubStore{rows: rowsAt(3)}

	page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, nil, 0)
	if err != nil {
		t.Fatalf("List: got %v, want no error", err)
	}
	if len(page.Items) != 1 || page.Next == nil {
		t.Fatalf("got %d rows and next %v, want one row and a cursor", len(page.Items), page.Next)
	}
}

// TestTheListingDoesNotSwallowAFailure separates an empty inbox from a database that did not answer.
func TestTheListingDoesNotSwallowAFailure(t *testing.T) {
	boom := errors.New("connection refused")

	_, err := NewService(Options{Repo: &stubStore{err: boom}}).List(context.Background(), adminScope, nil, 20)
	if !errors.Is(err, boom) {
		t.Fatalf("List: got %v, want the underlying failure", err)
	}
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: id.New(), OperatorID: operatorID, Role: role}
}

// TestWhoMayAnswerIsTheHolderOrThePool is decision 5 of the phase as a table: the conversation is the caller's or nobody's, for
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

			message, err := d.service.Send(context.Background(), scopeOf(me, tc.role), target.ConversationID, "  te paso la dirección  ")
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

	_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola")
	if !errors.Is(err, ErrTaken) {
		t.Fatalf("Send: got %v, want ErrTaken", err)
	}
	if len(d.aggregate.recorded) != 0 || d.sender.calls != 0 || len(d.store.assignments) != 0 {
		t.Fatalf("the loser wrote or sent: %d rows, %d sends, %d assignments",
			len(d.aggregate.recorded), d.sender.calls, len(d.store.assignments))
	}
}

// TestAnAnswerMetaWouldRefuseIsRefusedBeforeItIsWritten covers the refusals that come from the conversation and not from the
// caller: none of them leaves a row, because a row Meta was always going to reject is a failure nobody caused.
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

			_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, tc.text)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Send: got %v, want %v", err, tc.wantErr)
			}
			if len(d.aggregate.recorded) != 0 || d.sender.calls != 0 || len(d.aggregate.claimed) != 0 {
				t.Fatalf("a refused answer still wrote, claimed or sent")
			}
		})
	}

	d := newDesk(&stubStore{})
	if _, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), "hola"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Send to a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}

// TestAFailedSendIsTheRowSaidFailedAndNeverRetried is ADR-0020 from the operator's side: one attempt, the failure on the row and in
// the answer, and the conversation still taken, because the failure needs an owner to decide about it.
func TestAFailedSendIsTheRowSaidFailedAndNeverRetried(t *testing.T) {
	refused := errors.New("httpclient: status 400: recipient not in allowed list")

	for _, tc := range []struct {
		name  string
		shape func(*desk)
	}{
		{"Meta refuses the send", func(d *desk) { d.sender.err = refused }},
		{"the number cannot be read before sending", func(d *desk) { d.channel.err = refused }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			tc.shape(d)
			me := id.New()
			target := d.open(nil)

			message, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola")
			if err != nil {
				t.Fatalf("Send: got %v, want the failed row and no error", err)
			}
			if message.Status == nil || *message.Status != enums.MessageStatusFailed {
				t.Fatalf("status %v, want failed", message.Status)
			}
			if message.ErrorCode == nil || *message.ErrorCode != refused.Error() {
				t.Fatalf("error_code %v, want the cause", message.ErrorCode)
			}
			if d.aggregate.failed[message.ID] != refused.Error() {
				t.Fatalf("the row was not marked failed: %v", d.aggregate.failed)
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

// TestADeliveredAnswerIsNeverReportedAsAnError covers the write after a successful send failing: answering an error would invite
// the operator to send it again, which is the duplicate ADR-0020 exists to prevent.
func TestADeliveredAnswerIsNeverReportedAsAnError(t *testing.T) {
	d := newDesk(&stubStore{})
	d.aggregate.markSentErr = errors.New("connection reset")
	target := d.open(nil)

	message, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola")
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
	message, err = d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola")
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

	message, err := d.service.Send(ctx, scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola")
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

			_, err := d.service.Send(context.Background(), scopeOf(id.New(), enums.OperatorRoleOperator), target.ConversationID, "hola")
			if !errors.Is(err, boom) {
				t.Fatalf("Send: got %v, want the underlying failure", err)
			}
			if d.sender.calls != 0 {
				t.Fatalf("an answer with no row reached the customer")
			}
		})
	}
}

// TestTheModeChangesOnlyWhenItMakesSense is F27 as this phase offers it: off and back on, never suggestion before its cycle exists,
// never on for a conversation born without an agent, and nothing written for a change that changes nothing.
func TestTheModeChangesOnlyWhenItMakesSense(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shape     func(*Target)
		mode      enums.AgentMode
		wantErr   error
		wantWrite bool
	}{
		{"turning the bot off", func(*Target) {}, enums.AgentModeOff, nil, true},
		{"turning it back on", func(t *Target) { t.Mode = enums.AgentModeOff }, enums.AgentModeAutonomous, nil, true},
		{"asking for the mode it already has writes nothing", func(*Target) {}, enums.AgentModeAutonomous, nil, false},
		{"suggestion waits for Fase 9: stored now, the orchestrator would read it as silence",
			func(*Target) {}, enums.AgentModeSuggestion, ErrModeNotAvailable, false},
		{"a word that is not a mode of F02", func(*Target) {}, enums.AgentMode("robot"), ErrUnknownMode, false},
		{"a conversation born without an agent has no bot to turn on",
			func(t *Target) { t.AIAgentID, t.Mode = nil, enums.AgentModeOff }, enums.AgentModeAutonomous, ErrNoAgent, false},
		{"a resolved conversation is a closed unit of work (ADR-0002)",
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, enums.AgentModeOff, ErrResolved, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			got, err := d.service.SetMode(context.Background(), scopeOf(id.New(), enums.OperatorRoleSupervisor), target.ConversationID, tc.mode)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetMode: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.mode {
				t.Fatalf("answered mode %q, want %q", got, tc.mode)
			}
			if _, written := d.aggregate.modes[target.ConversationID]; written != tc.wantWrite {
				t.Fatalf("written = %v, want %v", written, tc.wantWrite)
			}
			// Turning the bot off is not answering, so it never takes the pool (ADR-0004).
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 {
				t.Fatalf("changing the mode assigned the conversation")
			}
		})
	}
}

// TestTheModeReportsWhatTheAggregateSaid covers the race the predicate settles: the conversation lost its agent or its row between
// the read and the write, and the caller hears the reason and not a 500.
func TestTheModeReportsWhatTheAggregateSaid(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"the predicate found no agent", conversations.ErrNoAgent, ErrNoAgent},
		{"the row was gone", conversations.ErrNotFound, ErrNotFound},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.setModeErr = tc.err
			target := d.open(nil)

			_, err := d.service.SetMode(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID, enums.AgentModeOff)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetMode: got %v, want %v", err, tc.wantErr)
			}
		})
	}

	d := newDesk(&stubStore{})
	if _, err := d.service.SetMode(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), enums.AgentModeOff); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetMode on a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}
