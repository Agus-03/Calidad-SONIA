package reminders

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: id.New(), OperatorID: operatorID, Role: role}
}

// TestAReminderHangsFromAContactTheCallerReaches is ADR-0058 for the create: a contact in reach, a future date, a text.
func TestAReminderHangsFromAContactTheCallerReaches(t *testing.T) {
	me, contact, theirs := id.New(), id.New(), id.New()
	conversation, elsewhere := id.New(), id.New()
	later := clock.Add(time.Hour)

	for _, tc := range []struct {
		name         string
		contact      uuid.UUID
		conversation *uuid.UUID
		description  string
		dueAt        time.Time
		wantErr      error
	}{
		{"on a contact it reaches", contact, nil, "llamar por la seña", later, nil},
		{"pointing at one of its conversations", contact, &conversation, "llamar", later, nil},
		{"a contact out of reach does not exist", theirs, nil, "llamar", later, ErrContactNotFound},
		{"a conversation of another contact", contact, &elsewhere, "llamar", later, ErrConversationNotOfContact},
		{"with no text", contact, nil, "   ", later, ErrEmptyDescription},
		{"due now is already past", contact, nil, "llamar", clock, ErrDueInThePast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore()
			store.conversations[conversation] = contact
			store.conversations[elsewhere] = theirs
			s := newService(store, contact)

			r, err := s.Create(context.Background(), scopeOf(me, enums.OperatorRoleOperator), tc.contact, tc.conversation, tc.description, tc.dueAt)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Create: got %v, want %v", err, tc.wantErr)
			}
			if (len(store.rows) == 1) != (tc.wantErr == nil) {
				t.Fatalf("%d rows written, want one only when allowed", len(store.rows))
			}
			if tc.wantErr == nil && (r.OperatorID != me || r.ContactID != tc.contact || r.Description != "llamar por la seña" && r.Description != "llamar") {
				t.Fatalf("the reminder %+v is not the caller's on that contact", r)
			}
		})
	}
}

// TestOnlyItsOwnerAndTheRolesAboveChangeAReminder: the owner, a supervisor or an admin; another operator is refused (ADR-0058).
func TestOnlyItsOwnerAndTheRolesAboveChangeAReminder(t *testing.T) {
	owner, other := id.New(), id.New()
	text := "nuevo texto"

	for _, tc := range []struct {
		name    string
		caller  uuid.UUID
		role    enums.OperatorRole
		wantErr error
	}{
		{"its owner", owner, enums.OperatorRoleOperator, nil},
		{"a supervisor", other, enums.OperatorRoleSupervisor, nil},
		{"an admin", other, enums.OperatorRoleAdmin, nil},
		{"another operator", other, enums.OperatorRoleOperator, ErrNotTheOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore()
			r := Reminder{ID: id.New(), ContactID: id.New(), OperatorID: owner, Description: "viejo", DueAt: clock.Add(time.Hour)}
			store.rows[r.ID] = r
			s := newService(store)

			if _, err := s.Update(context.Background(), scopeOf(tc.caller, tc.role), r.ID, &text, nil); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Update: got %v, want %v", err, tc.wantErr)
			}
			if err := s.Delete(context.Background(), scopeOf(tc.caller, tc.role), r.ID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Delete: got %v, want %v", err, tc.wantErr)
			}
			if _, kept := store.rows[r.ID]; kept != (tc.wantErr != nil) {
				t.Fatalf("kept = %v, want deleted only when allowed", kept)
			}
		})
	}
}

// TestMovingTheDateMakesItPendingAgain: a reminder already told and moved forward fires once more; a text edit does not (ADR-0058).
func TestMovingTheDateMakesItPendingAgain(t *testing.T) {
	owner := id.New()
	told := clock.Add(-time.Minute)
	store := newStore()
	r := Reminder{ID: id.New(), ContactID: id.New(), OperatorID: owner, Description: "llamar", DueAt: clock.Add(-time.Hour), Notified: true, NotifiedAt: &told}
	store.rows[r.ID] = r
	s := newService(store)
	scope := scopeOf(owner, enums.OperatorRoleOperator)

	text := "llamar a la tarde"
	edited, err := s.Update(context.Background(), scope, r.ID, &text, nil)
	if err != nil || !edited.Notified || edited.Description != text {
		t.Fatalf("a text edit of a due reminder: got %+v and %v, want it still told and allowed", edited, err)
	}
	past := clock.Add(-time.Minute)
	if _, err := s.Update(context.Background(), scope, r.ID, nil, &past); !errors.Is(err, ErrDueInThePast) {
		t.Fatalf("moving it to the past: got %v, want ErrDueInThePast", err)
	}
	future := clock.Add(24 * time.Hour)
	moved, err := s.Update(context.Background(), scope, r.ID, nil, &future)
	if err != nil || moved.Notified || moved.NotifiedAt != nil || !moved.DueAt.Equal(future) {
		t.Fatalf("moving it forward: got %+v and %v, want it pending again", moved, err)
	}
	blank := " "
	if _, err := s.Update(context.Background(), scope, r.ID, &blank, nil); !errors.Is(err, ErrEmptyDescription) {
		t.Fatalf("an edit to no text: got %v, want ErrEmptyDescription", err)
	}
}

// TestMineIsWhatReachesTheCaller: what the vendor of the contact is told, not what the caller wrote (ADR-0058).
func TestMineIsWhatReachesTheCaller(t *testing.T) {
	writer, vendor := id.New(), id.New()
	store := newStore()
	due := Reminder{ID: id.New(), ContactID: id.New(), OperatorID: writer, Description: "vencido", DueAt: time.Now().Add(-time.Hour)}
	coming := Reminder{ID: id.New(), ContactID: id.New(), OperatorID: writer, Description: "próximo", DueAt: time.Now().Add(time.Hour)}
	store.rows[due.ID], store.rows[coming.ID] = due, coming
	store.addressed[due.ID], store.addressed[coming.ID] = vendor, vendor
	s := newService(store)

	all, _ := s.Mine(context.Background(), scopeOf(vendor, enums.OperatorRoleOperator), false)
	onlyDue, _ := s.Mine(context.Background(), scopeOf(vendor, enums.OperatorRoleOperator), true)
	ofWriter, _ := s.Mine(context.Background(), scopeOf(writer, enums.OperatorRoleOperator), false)
	if len(all) != 2 || len(onlyDue) != 1 || onlyDue[0].ID != due.ID || len(ofWriter) != 0 {
		t.Fatalf("the vendor reads %d and %d due, the writer %d; want 2, 1 and 0", len(all), len(onlyDue), len(ofWriter))
	}
	if _, err := s.OfContact(context.Background(), scopeOf(vendor, enums.OperatorRoleOperator), due.ContactID); !errors.Is(err, ErrContactNotFound) {
		t.Fatalf("the reminders of a contact out of reach: got %v, want ErrContactNotFound", err)
	}
}

// TestTheTickTellsEachRecipient: the poller tells each due reminder to its person once, and a failed read tells nobody.
func TestTheTickTellsEachRecipient(t *testing.T) {
	first, second := id.New(), id.New()
	store := newStore()
	store.due = []Due{{Reminder: Reminder{ID: id.New()}, Recipient: first}, {Reminder: Reminder{ID: id.New()}, Recipient: second}}
	alerts := &stubAlerts{}
	p := NewPoller(PollerOptions{Store: store, Alerts: alerts, Logger: slog.New(slog.DiscardHandler)})

	p.tick(context.Background())
	if len(alerts.told) != 2 || alerts.told[0] != first || alerts.told[1] != second {
		t.Fatalf("told %v, want each recipient once in order", alerts.told)
	}

	store.dueErr = errors.New("connection refused")
	p.tick(context.Background())
	if len(alerts.told) != 2 {
		t.Fatalf("a failed read told %v", alerts.told[2:])
	}
}

// TestThePollerStopsWithItsContext is the ordered shutdown: cancelling the loop lets Wait return (ADR-0029).
func TestThePollerStopsWithItsContext(t *testing.T) {
	store := newStore()
	alerts := &stubAlerts{}
	store.due = []Due{{Reminder: Reminder{ID: id.New()}, Recipient: id.New()}}
	p := NewPoller(PollerOptions{Store: store, Alerts: alerts, Interval: 5 * time.Millisecond, Logger: slog.New(slog.DiscardHandler)})
	if p.opts.Limit != defaultLimit {
		t.Fatalf("the limit is %d, want the default %d", p.opts.Limit, defaultLimit)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		alerts.mu.Lock()
		n := len(alerts.told)
		alerts.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	waitCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := p.Wait(waitCtx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(alerts.told) == 0 {
		t.Fatal("the loop never ticked")
	}
	if NewPoller(PollerOptions{}).opts.Interval != defaultInterval {
		t.Fatal("a zero interval did not take the default")
	}
}
