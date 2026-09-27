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
// hands out a cursor to nothing, and the frontend asks for an empty page to learn that it is done (ADR-0036, regla 7).
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

			page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, Filter{}, nil, tc.limit)
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
				if page.Next.ID != last.ID || !page.Next.At.Equal(last.LastActivityAt) {
					t.Fatalf("the cursor names %+v, want the last row shown, %v", page.Next, last.ID)
				}
			}
		})
	}
}

// TestAPageOfZeroIsReadAsAPageOfOne keeps a caller that forgot the limit from indexing the row before the first one.
func TestAPageOfZeroIsReadAsAPageOfOne(t *testing.T) {
	store := &stubStore{rows: rowsAt(3)}

	page, err := NewService(Options{Repo: store}).List(context.Background(), adminScope, Filter{}, nil, 0)
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

	_, err := NewService(Options{Repo: &stubStore{err: boom}}).List(context.Background(), adminScope, Filter{}, nil, 20)
	if !errors.Is(err, boom) {
		t.Fatalf("List: got %v, want the underlying failure", err)
	}
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: id.New(), OperatorID: operatorID, Role: role}
}

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

// TestTheThreadHasTheReachOfTheListing is the listing's rule one conversation at a time: an operator reads its own and the pool, the
// two roles above it read the whole team, and an id guessed past the listing is answered as missing (ADR-0035, reglas 2 y 3).
func TestTheThreadHasTheReachOfTheListing(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		wantErr error
	}{
		{"an operator reads its own conversation", &me, enums.OperatorRoleOperator, nil},
		{"an operator reads the pool, which it has to read before taking it", nil, enums.OperatorRoleOperator, nil},
		{"an operator is told another's conversation does not exist", &other, enums.OperatorRoleOperator, ErrNotFound},
		{"a supervisor reads the whole team (Anexo B)", &other, enums.OperatorRoleSupervisor, nil},
		{"an admin reads the whole team", &other, enums.OperatorRoleAdmin, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			want := d.write(target.ConversationID, 2)

			page, err := d.service.Thread(context.Background(), scopeOf(me, tc.role), target.ConversationID, nil, 20)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Thread: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if len(page.Items) != 2 || page.Items[0].ID != want[0].ID || page.Next != nil {
				t.Fatalf("got %d messages, next %v, want the two newest first and no cursor", len(page.Items), page.Next)
			}
		})
	}
}

// TestTheThreadLoadsWhatCameBeforeOnePageAtATime walks the cursor through the service: newest first, each page after the last one
// shown, and no cursor once the oldest message is on screen.
func TestTheThreadLoadsWhatCameBeforeOnePageAtATime(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(nil)
	want := d.write(target.ConversationID, 5)
	scope := scopeOf(id.New(), enums.OperatorRoleOperator)

	seen := []uuid.UUID{}
	var after *Cursor
	for pages := 0; pages < 5; pages++ {
		page, err := d.service.Thread(context.Background(), scope, target.ConversationID, after, 2)
		if err != nil {
			t.Fatalf("Thread: %v", err)
		}
		if d.store.lastLimit != 3 {
			t.Fatalf("the store was asked for %d messages, want one past the page", d.store.lastLimit)
		}
		for _, m := range page.Items {
			seen = append(seen, m.ID)
		}
		if page.Next == nil {
			break
		}
		after = page.Next
	}

	if len(seen) != len(want) {
		t.Fatalf("walked %d messages, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i].ID {
			t.Fatalf("message %d is %v, want %v: newest first, nothing repeated", i, seen[i], want[i].ID)
		}
	}

	if page, err := d.service.Thread(context.Background(), scope, target.ConversationID, nil, 0); err != nil || len(page.Items) != 1 {
		t.Fatalf("a page of zero: got %d messages and %v, want one", len(page.Items), err)
	}
}

// TestTheThreadDoesNotSwallowAFailure separates an empty thread from a database that did not answer.
func TestTheThreadDoesNotSwallowAFailure(t *testing.T) {
	boom := errors.New("connection refused")

	d := newDesk(&stubStore{})
	target := d.open(nil)
	d.store.threadErr = boom
	if _, err := d.service.Thread(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID, nil, 20); !errors.Is(err, boom) {
		t.Fatalf("Thread: got %v, want the underlying failure", err)
	}

	d = newDesk(&stubStore{})
	if _, err := d.service.Thread(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), nil, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Thread of a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}

// TestTheModeChangesOnlyWhenItMakesSense is F27 as this phase offers it: off and back on, never suggestion before its cycle exists,
// never on for a conversation born without an agent, and nothing written for a change that changes nothing (ADR-0035, reglas 11 a 14).
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
		{"suggestion is one of the three since ADR-0046 amended regla 11",
			func(*Target) {}, enums.AgentModeSuggestion, nil, true},
		{"a word that is not a mode of F02", func(*Target) {}, enums.AgentMode("robot"), ErrUnknownMode, false},
		{"a conversation born without an agent has no bot to turn on",
			func(t *Target) { t.AIAgentID, t.Mode = nil, enums.AgentModeOff }, enums.AgentModeAutonomous, ErrNoAIAgent, false},
		{"an agent paused or removed by the admin cannot be turned on: the turn would skip it and nobody would answer (ADR-0038)",
			func(t *Target) { t.Mode, t.AIAgentAvailable = enums.AgentModeOff, false }, enums.AgentModeAutonomous, ErrAIAgentUnavailable, false},
		{"turning off never needs an agent that can answer",
			func(t *Target) { t.AIAgentAvailable = false }, enums.AgentModeOff, nil, true},
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
			// Turning the bot off is not answering, so it never takes the pool (ADR-0035, regla 13).
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

	// Every case turns on a bot that is off and whose agent could answer when it was read: the only request that reaches the
	// predicate with something to refuse, since turning off never needs an agent and the read already refused one that was missing.
	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"the predicate refused an agent paused or removed after the read (ADR-0038)", conversations.ErrNoAIAgent, ErrAIAgentUnavailable},
		{"the row was gone", conversations.ErrNotFound, ErrNotFound},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.setModeErr = tc.err
			target := d.open(nil)
			target.Mode = enums.AgentModeOff
			d.store.targets[target.ConversationID] = target

			_, err := d.service.SetMode(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID, enums.AgentModeAutonomous)
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

// TestTheStarHasTheReachOfTheThread is F25 as a table: whoever reads the conversation stars it, whatever its status, what the caller
// cannot read does not exist, and nothing is written for a star it already has (ADR-0035, reglas 2 y 3).
func TestTheStarHasTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name      string
		holder    *uuid.UUID
		role      enums.OperatorRole
		shape     func(*Target)
		starred   bool
		wantErr   error
		wantWrite bool
	}{
		{"an operator stars its own conversation", &me, enums.OperatorRoleOperator, func(*Target) {}, true, nil, true},
		{"an operator stars one of the pool, which it reads", nil, enums.OperatorRoleOperator, func(*Target) {}, true, nil, true},
		{"another's conversation does not exist for an operator", &other, enums.OperatorRoleOperator, func(*Target) {}, true, ErrNotFound, false},
		{"a supervisor stars any conversation of the team", &other, enums.OperatorRoleSupervisor, func(*Target) {}, true, nil, true},
		{"a resolved conversation is starred for its follow-up", &me, enums.OperatorRoleOperator,
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, true, nil, true},
		{"the star is cleared", &me, enums.OperatorRoleOperator, func(t *Target) { t.Starred = true }, false, nil, true},
		{"asking for the star it already has writes nothing", &me, enums.OperatorRoleOperator,
			func(t *Target) { t.Starred = true }, true, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			got, err := d.service.SetStarred(context.Background(), scopeOf(me, tc.role), target.ConversationID, tc.starred)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetStarred: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && got != tc.starred {
				t.Fatalf("answered starred %v, want %v", got, tc.starred)
			}
			if _, written := d.aggregate.starred[target.ConversationID]; written != tc.wantWrite {
				t.Fatalf("written = %v, want %v", written, tc.wantWrite)
			}
			// A star is not an answer, so it never takes the pool (ADR-0035, regla 13).
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 {
				t.Fatal("starring assigned the conversation")
			}
		})
	}
}

// TestTheStarReportsWhatTheAggregateSaid covers the row gone between the read and the write, and a database that did not answer.
func TestTheStarReportsWhatTheAggregateSaid(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"the row was gone", conversations.ErrNotFound, ErrNotFound},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.setStarredErr = tc.err
			target := d.open(nil)

			if _, err := d.service.SetStarred(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID, true); !errors.Is(err, tc.wantErr) {
				t.Fatalf("SetStarred: got %v, want %v", err, tc.wantErr)
			}
		})
	}

	d := newDesk(&stubStore{})
	if _, err := d.service.SetStarred(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New(), true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetStarred on a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}

// TestWhoMayClearTheUnreadIsWhoMayAnswer is ADR-0052 as a table: the holder, or anybody for the pool, clears it; a supervisor
// looking at a vendor's conversation is told it is taken and clears nothing; an operator is told another's does not exist. Nothing
// unread writes nothing and tells nobody, and a resolved conversation is read like any other.
func TestWhoMayClearTheUnreadIsWhoMayAnswer(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name      string
		holder    *uuid.UUID
		role      enums.OperatorRole
		shape     func(*Target)
		wantErr   error
		wantWrite bool
	}{
		{"the holder clears it", &me, enums.OperatorRoleOperator, func(*Target) {}, nil, true},
		{"anybody clears one of the pool", nil, enums.OperatorRoleOperator, func(*Target) {}, nil, true},
		{"a supervisor does not clear a vendor's warning", &other, enums.OperatorRoleSupervisor, func(*Target) {}, ErrTaken, false},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, func(*Target) {}, ErrNotFound, false},
		{"a resolved one is read like any other", &me, enums.OperatorRoleOperator,
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, nil, true},
		{"nothing unread writes nothing", &me, enums.OperatorRoleOperator, func(t *Target) { t.UnreadCount = 0 }, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			target.UnreadCount = 3
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			err := d.service.MarkRead(context.Background(), scopeOf(me, tc.role), target.ConversationID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("MarkRead: got %v, want %v", err, tc.wantErr)
			}
			if written := len(d.aggregate.read) == 1; written != tc.wantWrite {
				t.Fatalf("written = %v, want %v", written, tc.wantWrite)
			}
			// Told only when something changed, and reading never takes the pool (ADR-0035, regla 13).
			if told := len(d.events.told) == 1; told != tc.wantWrite {
				t.Fatalf("told the hub %v, want %v", d.events.told, tc.wantWrite)
			}
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 {
				t.Fatal("marking read assigned the conversation")
			}
		})
	}
}

// TestTheReadReportsWhatTheAggregateSaid covers the row gone between the read and the write, and a database that did not answer.
func TestTheReadReportsWhatTheAggregateSaid(t *testing.T) {
	boom := errors.New("connection refused")
	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"the row was gone", conversations.ErrNotFound, ErrNotFound},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.readErr = tc.err
			target := d.open(nil)
			target.UnreadCount = 1
			d.store.targets[target.ConversationID] = target

			if err := d.service.MarkRead(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("MarkRead: got %v, want %v", err, tc.wantErr)
			}
			if len(d.events.told) != 0 {
				t.Fatalf("a failed read told the hub %v", d.events.told)
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
