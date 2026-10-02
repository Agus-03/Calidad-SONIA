package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheServiceTellsTheHubAfterEachWrite: the answer that takes the pool says so, the others say changed, and a refused request
// tells nothing, because nothing was written (ADR-0051).
func TestTheServiceTellsTheHubAfterEachWrite(t *testing.T) {
	me := id.New()
	for _, tc := range []struct {
		name string
		do   func(d *desk, target Target) error
		held bool
		want []string
	}{
		{"answering the pool", func(d *desk, target Target) error {
			_, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
			return err
		}, false, []string{"left pool"}},
		{"answering its own", func(d *desk, target Target) error {
			_, err := d.service.Send(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, "hola", nil)
			return err
		}, true, []string{"changed"}},
		{"switching the bot", func(d *desk, target Target) error {
			_, err := d.service.SetMode(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, enums.AgentModeOff)
			return err
		}, true, []string{"changed"}},
		{"starring", func(d *desk, target Target) error {
			_, err := d.service.SetStarred(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, true)
			return err
		}, true, []string{"changed"}},
		{"a refused switch tells nothing", func(d *desk, target Target) error {
			_, err := d.service.SetMode(context.Background(), scopeOf(me, enums.OperatorRoleOperator), target.ConversationID, enums.AgentModeOff)
			if !errors.Is(err, ErrNotHeld) {
				return errors.New("want the pool refused")
			}
			return nil
		}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			var holder *uuid.UUID
			if tc.held {
				holder = &me
			}
			target := d.open(holder)
			if err := tc.do(d, target); err != nil {
				t.Fatalf("%v", err)
			}
			got := d.events.told
			if len(got) != len(tc.want) {
				t.Fatalf("told %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("told %v, want %v", got, tc.want)
				}
			}
		})
	}
}
