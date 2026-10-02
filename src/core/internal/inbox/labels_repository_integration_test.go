package inbox

import (
	"context"
	"testing"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestLabellingAgainstRealDDL is ADR-0057 against real DDL: one link per label, the row and the filter see it, a retired label does not.
func TestLabellingAgainstRealDDL(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)
		label := id.New()
		if _, err := exec.Exec(ctx, `INSERT INTO labels (id, account_id, name, color, created_by_operator_id) VALUES ($1, $2, $3, '#00ff00', $4)`,
			label, w.account, "F24 "+label.String(), w.mine); err != nil {
			t.Fatalf("a label: %v", err)
		}

		for i, want := range []bool{true, false} {
			if changed, err := repo.AssignLabel(ctx, id.New(), w.mineConv, label, w.mine); err != nil || changed != want {
				t.Fatalf("AssignLabel #%d: got %v and %v, want changed = %v", i+1, changed, err, want)
			}
		}
		tags, err := repo.LabelsOf(ctx, w.mineConv)
		if err != nil || len(tags) != 1 || tags[0].ID != label || tags[0].Color != "#00ff00" {
			t.Fatalf("LabelsOf: got %+v and %v, want the one label", tags, err)
		}
		_, row, err := repo.SummaryOf(ctx, w.mineConv)
		if err != nil || len(row.Labels) != 1 || row.Labels[0].ID != label {
			t.Fatalf("the row carries %+v (%v), want the label on it", row.Labels, err)
		}
		listed, err := repo.List(ctx, Scope{AccountID: w.account, OperatorID: w.mine, Role: enums.OperatorRoleAdmin}, Filter{LabelID: &label}, nil, 1000)
		if err != nil || len(listed) != 1 || listed[0].ID != w.mineConv {
			t.Fatalf("the filter by label holds %v (%v), want only the labelled conversation", idsOf(listed), err)
		}

		// A label retired from the catalogue leaves every row, like it leaves the filter (ADR-0050, regla 5).
		if _, err := exec.Exec(ctx, `UPDATE labels SET deleted_at = now() WHERE id = $1`, label); err != nil {
			t.Fatalf("retire the label: %v", err)
		}
		if _, row, err := repo.SummaryOf(ctx, w.mineConv); err != nil || len(row.Labels) != 0 {
			t.Fatalf("the row carries %+v (%v) after the label was retired, want none", row.Labels, err)
		}
		for i, want := range []bool{true, false} {
			if changed, err := repo.UnassignLabel(ctx, w.mineConv, label); err != nil || changed != want {
				t.Fatalf("UnassignLabel #%d: got %v and %v, want changed = %v", i+1, changed, err, want)
			}
		}
	})
}
