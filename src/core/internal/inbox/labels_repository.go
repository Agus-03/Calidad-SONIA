package inbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ON CONFLICT DO NOTHING and not a refusal: tagging twice is the same request twice, and the row already says what was asked (ADR-0057).
const conversationLabelInsert = `
INSERT INTO conversation_labels (id, conversation_id, label_id, assigned_by_operator_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id, label_id) DO NOTHING`

// AssignLabel answers whether a link was written, so only a real change is told to the hub.
func (r *Repository) AssignLabel(ctx context.Context, linkID, conversationID, labelID, operatorID uuid.UUID) (bool, error) {
	tag, err := r.exec.Exec(ctx, conversationLabelInsert, linkID, conversationID, labelID, operatorID)
	if err != nil {
		return false, fmt.Errorf("inbox: assign label: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// A DELETE, the exception to C09 the schema declares for this table: unlabelling is routine and no metric counts it.
func (r *Repository) UnassignLabel(ctx context.Context, conversationID, labelID uuid.UUID) (bool, error) {
	tag, err := r.exec.Exec(ctx, `DELETE FROM conversation_labels WHERE conversation_id = $1 AND label_id = $2`, conversationID, labelID)
	if err != nil {
		return false, fmt.Errorf("inbox: unassign label: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// The same read as the row of the listing, so the answer of a tag and the event that follows it cannot disagree (ADR-0057).
const conversationLabelsSelect = `
SELECT l.id, l.name, l.color
FROM   conversation_labels cl
JOIN   labels l ON l.id = cl.label_id AND l.deleted_at IS NULL
WHERE  cl.conversation_id = $1
ORDER  BY l.name, l.id`

func (r *Repository) LabelsOf(ctx context.Context, conversationID uuid.UUID) ([]Label, error) {
	rows, err := r.exec.Query(ctx, conversationLabelsSelect, conversationID)
	if err != nil {
		return nil, fmt.Errorf("inbox: read the labels: %w", err)
	}
	defer rows.Close()

	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, fmt.Errorf("inbox: scan a label: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
