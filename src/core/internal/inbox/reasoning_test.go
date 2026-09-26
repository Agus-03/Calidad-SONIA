package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// assembled is a generation of the conversation with two components, the texts written by the admin.
func assembled(d *desk, conversationID uuid.UUID) uuid.UUID {
	generationID := id.New()
	objective, rules := "Alquilar inmuebles en Córdoba.", "No tomar pagos por este canal."
	if d.store.reasonings == nil {
		d.store.reasonings = map[uuid.UUID]stubReasoning{}
	}
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

// ADR-0049: every role sees which components the answer used, and only the admin reads what they say, because only the admin has
// access to the configuration they come from.
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
			d := newDesk(&stubStore{})
			target := d.open(&me)
			generationID := assembled(d, target.ConversationID)

			got, err := d.service.Reasoning(context.Background(), scopeOf(me, tc.role), target.ConversationID, generationID)
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
	d := newDesk(&stubStore{})
	target := d.open(&me)
	generationID := assembled(d, target.ConversationID)

	if _, err := d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, generationID); err != nil {
		t.Fatalf("Reasoning: %v", err)
	}
	if d.store.reasonings[generationID].Components[0].Content == nil {
		t.Fatal("the text was removed from the store, and the admin would now read nothing")
	}
}

// The reach of the thread (ADR-0035, regla 2): what the Operator cannot read it cannot ask about, and it is told the conversation
// does not exist; a generation of another conversation is not found, whatever id the request carried.
func TestTheReasoningIsReadWithTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()
	d := newDesk(&stubStore{})
	mine := d.open(&me)
	theirs := d.open(&other)
	ofTheirs := assembled(d, theirs.ConversationID)

	_, err := d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), theirs.ConversationID, ofTheirs)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("a conversation held by another: got %v, want ErrNotFound", err)
	}
	_, err = d.service.Reasoning(context.Background(), scopeOf(me, enums.OperatorRoleOperator), mine.ConversationID, ofTheirs)
	if !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("a generation of another conversation: got %v, want ErrGenerationNotFound", err)
	}
}
