package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

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

// TestWhoMayCloseIsWhoMaySwitchTheBot is ADR-0054 as a table: the vendor closes its own, the roles above any, the pool is taken first.
func TestWhoMayCloseIsWhoMaySwitchTheBot(t *testing.T) {
	me, other := id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		shape   func(*Target)
		wantErr error
	}{
		{"the vendor closes its own", &me, enums.OperatorRoleOperator, func(*Target) {}, nil},
		{"a supervisor closes any of the team", &other, enums.OperatorRoleSupervisor, func(*Target) {}, nil},
		{"an admin closes one of the pool", nil, enums.OperatorRoleAdmin, func(*Target) {}, nil},
		{"a waiting one is closed too", &me, enums.OperatorRoleOperator, func(t *Target) { t.Status = enums.ConversationStatusWaiting }, nil},
		{"an operator takes the pool before closing it", nil, enums.OperatorRoleOperator, func(*Target) {}, ErrNotHeld},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, func(*Target) {}, ErrNotFound},
		{"a resolved one never reopens, so it is not closed twice (ADR-0002)", &me, enums.OperatorRoleOperator,
			func(t *Target) { t.Status = enums.ConversationStatusResolved }, ErrResolved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			err := d.service.Resolve(context.Background(), scopeOf(me, tc.role), target.ConversationID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Resolve: got %v, want %v", err, tc.wantErr)
			}
			closer, written := d.aggregate.resolvedBy[target.ConversationID]
			if written != (tc.wantErr == nil) {
				t.Fatalf("written = %v, want %v", written, tc.wantErr == nil)
			}
			if written && closer != me {
				t.Fatalf("closed by %v, want the caller %v", closer, me)
			}
			if written && d.aggregate.resolveBudget != testTurnBudget {
				t.Fatalf("closed with a budget of %v, want the configured %v", d.aggregate.resolveBudget, testTurnBudget)
			}
			if told := len(d.events.told) == 1; told != written {
				t.Fatalf("told the hub %v, want only after a close", d.events.told)
			}
			// Closing is not answering: it never takes the pool, and it leaves the mode alone (ADR-0035 regla 13, ADR-0054).
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 || len(d.aggregate.modes) != 0 {
				t.Fatal("closing assigned the conversation or switched its bot")
			}
		})
	}
}

// TestTheCloseReportsWhatTheAggregateSaid covers a close that committed between the read and the write, and a database that did not answer.
func TestTheCloseReportsWhatTheAggregateSaid(t *testing.T) {
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"somebody closed it in between", conversations.ErrResolved, ErrResolved},
		{"the bot is still answering", conversations.ErrTurnInProgress, ErrTurnInProgress},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.resolveErr = tc.err
			target := d.open(nil)

			if err := d.service.Resolve(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), target.ConversationID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Resolve: got %v, want %v", err, tc.wantErr)
			}
			if len(d.events.told) != 0 {
				t.Fatalf("told the hub %v after a close that did not happen", d.events.told)
			}
		})
	}

	d := newDesk(&stubStore{})
	if err := d.service.Resolve(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), id.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve on a conversation that does not exist: got %v, want ErrNotFound", err)
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

// TestOnlyTheVendorScoresAResolvedConversation is ADR-0055 as a table: the vendor scores what it held once closed, one to five.
func TestOnlyTheVendorScoresAResolvedConversation(t *testing.T) {
	me, other := id.New(), id.New()
	resolved := func(t *Target) { t.Status = enums.ConversationStatusResolved }

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		shape   func(*Target)
		score   int
		wantErr error
	}{
		{"the vendor scores its closed one", &me, enums.OperatorRoleOperator, resolved, 5, nil},
		{"a supervisor who held it scores it too", &me, enums.OperatorRoleSupervisor, resolved, 1, nil},
		{"zero is off the scale", &me, enums.OperatorRoleOperator, resolved, 0, ErrInvalidScore},
		{"six is off the scale", &me, enums.OperatorRoleOperator, resolved, 6, ErrInvalidScore},
		{"an open one is not scored", &me, enums.OperatorRoleOperator, func(*Target) {}, 3, ErrNotResolved},
		{"a waiting one is not scored", &me, enums.OperatorRoleOperator,
			func(t *Target) { t.Status = enums.ConversationStatusWaiting }, 3, ErrNotResolved},
		{"a supervisor does not score a vendor's", &other, enums.OperatorRoleSupervisor, resolved, 3, ErrNotTheVendor},
		{"an admin does not score a vendor's", &other, enums.OperatorRoleAdmin, resolved, 3, ErrNotTheVendor},
		{"nobody scores one closed in the pool", nil, enums.OperatorRoleOperator, resolved, 3, ErrNotTheVendor},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, resolved, 3, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			tc.shape(&target)
			d.store.targets[target.ConversationID] = target

			got, err := d.service.Score(context.Background(), scopeOf(me, tc.role), target.ConversationID, tc.score)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Score: got %v, want %v", err, tc.wantErr)
			}
			written, ok := d.aggregate.scores[target.ConversationID]
			if ok != (tc.wantErr == nil) {
				t.Fatalf("written = %v, want %v", ok, tc.wantErr == nil)
			}
			if ok && (written != tc.score || got != tc.score || d.aggregate.scoredBy[target.ConversationID] != me) {
				t.Fatalf("wrote %d by %v and answered %d, want %d by %v", written, d.aggregate.scoredBy[target.ConversationID], got, tc.score, me)
			}
			if told := len(d.events.told) == 1; told != ok {
				t.Fatalf("told the hub %v, want only after a score", d.events.told)
			}
			// Scoring is not answering: it never takes the pool (ADR-0035 regla 13, ADR-0055).
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 {
				t.Fatal("scoring assigned the conversation")
			}
		})
	}
}

// TestTheScoreReportsWhatTheAggregateSaid covers a row gone between the read and the write, and a database that did not answer.
func TestTheScoreReportsWhatTheAggregateSaid(t *testing.T) {
	me := id.New()
	boom := errors.New("connection refused")

	for _, tc := range []struct {
		name    string
		err     error
		wantErr error
	}{
		{"the row is gone", conversations.ErrNotFound, ErrNotFound},
		{"the database did not answer", boom, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.aggregate.scoreErr = tc.err
			target := d.open(&me)
			target.Status = enums.ConversationStatusResolved
			d.store.targets[target.ConversationID] = target

			if _, err := d.service.Score(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, 3); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Score: got %v, want %v", err, tc.wantErr)
			}
			if len(d.events.told) != 0 {
				t.Fatalf("told the hub %v after a score that did not happen", d.events.told)
			}
		})
	}

	d := newDesk(&stubStore{})
	if _, err := d.service.Score(context.Background(), scopeOf(me, enums.OperatorRoleAdmin), id.New(), 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Score on a conversation that does not exist: got %v, want ErrNotFound", err)
	}
}
