package reasoning

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount  = id.New()
	testOperator = id.New()
)

// stubReach is the rule inbox/ owns, imitated: the holder or the pool for an operator, any for the roles above (ADR-0035, regla 2).
type stubReach struct{ holders map[uuid.UUID]*uuid.UUID }

func (r *stubReach) Reachable(_ context.Context, scope inbox.Scope, conversationID uuid.UUID) error {
	holder, ok := r.holders[conversationID]
	if !ok || (scope.Role == enums.OperatorRoleOperator && holder != nil && *holder != scope.OperatorID) {
		return inbox.ErrNotFound
	}
	return nil
}

// stubReasoning is a generation and the conversation it belongs to, which the real read checks in its WHERE.
type stubReasoning struct {
	Reasoning
	conversationID uuid.UUID
}

type stubStore struct{ reasonings map[uuid.UUID]stubReasoning }

func (s *stubStore) Reasoning(_ context.Context, conversationID, generationID uuid.UUID) (Reasoning, error) {
	r, ok := s.reasonings[generationID]
	if !ok || r.conversationID != conversationID {
		return Reasoning{}, ErrGenerationNotFound
	}
	return r.Reasoning, nil
}

type desk struct {
	store   *stubStore
	reach   *stubReach
	service *Service
}

func newDesk() *desk {
	d := &desk{store: &stubStore{reasonings: map[uuid.UUID]stubReasoning{}}, reach: &stubReach{holders: map[uuid.UUID]*uuid.UUID{}}}
	d.service = NewService(d.store, d.reach)
	return d
}

func (d *desk) open(holder *uuid.UUID) uuid.UUID {
	conversationID := id.New()
	d.reach.holders[conversationID] = holder
	return conversationID
}

// assembled is a generation of the conversation with two components, the texts written by the admin.
func (d *desk) assembled(conversationID uuid.UUID) uuid.UUID {
	generationID := id.New()
	objective, rules := "Alquilar inmuebles en Córdoba.", "No tomar pagos por este canal."
	d.store.reasonings[generationID] = stubReasoning{
		conversationID: conversationID,
		Reasoning: Reasoning{
			GenerationID: generationID,
			Status:       enums.GenerationStatusAutoSent,
			LLMModel:     "command-a-03-2025",
			Components: []UsedComponent{
				{Type: enums.PromptComponentTypeObjective, Position: 1, VersionNumber: 3, Content: &objective},
				{Type: enums.PromptComponentTypeRules, Position: 2, VersionNumber: 1, Content: &rules},
			},
		},
	}
	return generationID
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) inbox.Scope {
	return inbox.Scope{AccountID: testAccount, OperatorID: operatorID, Role: role}
}

// ADR-0049: every role sees which components the answer used, and only the admin reads what they say.
func TestTheTextOfTheComponentsIsTheAdminsAlone(t *testing.T) {
	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		withText bool
	}{
		{"the operator sees which components and not what they say", enums.OperatorRoleOperator, false},
		{"the supervisor is on the side of the operator: the configuration is not theirs either", enums.OperatorRoleSupervisor, false},
		{"the admin, who writes the configuration, reads it", enums.OperatorRoleAdmin, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			me := id.New()
			d := newDesk()
			conversationID := d.open(&me)
			generationID := d.assembled(conversationID)

			got, err := d.service.Reasoning(context.Background(), scopeOf(me, tc.role), conversationID, generationID)
			if err != nil {
				t.Fatalf("Reasoning: %v", err)
			}
			if len(got.Components) != 2 || got.Components[0].Type != enums.PromptComponentTypeObjective || got.Components[1].VersionNumber != 1 {
				t.Fatalf("components = %+v, want the two in the order they were assembled, with their versions", got.Components)
			}
			for _, c := range got.Components {
				if (c.Content != nil) != tc.withText {
					t.Fatalf("component %s carries text %v, want %v", c.Type, c.Content != nil, tc.withText)
				}
			}
		})
	}
}

// Stripping the text for one caller must not strip it from what the store holds: the next caller may be the admin.
func TestHidingTheTextDoesNotTouchWhatTheStoreHolds(t *testing.T) {
	me := id.New()
	d := newDesk()
	conversationID := d.open(&me)
	generationID := d.assembled(conversationID)

	if _, err := d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), conversationID, generationID); err != nil {
		t.Fatalf("Reasoning: %v", err)
	}
	if d.store.reasonings[generationID].Components[0].Content == nil {
		t.Fatal("the text was removed from the store, and the admin would now read nothing")
	}
}

// The reach of the thread (ADR-0035, regla 2), asked of inbox/: what the Operator cannot read it cannot ask about.
func TestTheReasoningIsReadWithTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()
	d := newDesk()
	mine := d.open(&me)
	theirs := d.open(&other)
	ofTheirs := d.assembled(theirs)

	_, err := d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), theirs, ofTheirs)
	if !errors.Is(err, inbox.ErrNotFound) {
		t.Fatalf("a conversation held by another: got %v, want inbox.ErrNotFound", err)
	}
	_, err = d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), mine, ofTheirs)
	if !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("a generation of another conversation: got %v, want ErrGenerationNotFound", err)
	}
}
