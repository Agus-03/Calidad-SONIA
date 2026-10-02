package reasoning

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// The generation by its id and its conversation together, so an id of another conversation is not found rather than read.
const reasoningGeneration = `
SELECT g.id, g.status, g.llm_model, g.created_at
FROM   generations g
WHERE  g.id = $1 AND g.conversation_id = $2`

// No filter on deleted_at: the version is what was assembled, and removing the component later does not unsay it (C09, ADR-0039).
const reasoningComponents = `
SELECT   pc.type, gc.position, v.version_number, v.content
FROM     generation_components gc
JOIN     prompt_component_versions v ON v.id = gc.prompt_component_version_id
JOIN     prompt_components pc ON pc.id = v.prompt_component_id
WHERE    gc.generation_id = $1
ORDER BY gc.position`

// Reasoning reads the text of every version and leaves to the service who keeps it; another conversation's is ErrGenerationNotFound.
func (r *Repository) Reasoning(ctx context.Context, conversationID, generationID uuid.UUID) (Reasoning, error) {
	var out Reasoning
	err := r.exec.QueryRow(ctx, reasoningGeneration, generationID, conversationID).
		Scan(&out.GenerationID, &out.Status, &out.LLMModel, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reasoning{}, ErrGenerationNotFound
	}
	if err != nil {
		return Reasoning{}, fmt.Errorf("reasoning: read generation: %w", err)
	}

	rows, err := r.exec.Query(ctx, reasoningComponents, generationID)
	if err != nil {
		return Reasoning{}, fmt.Errorf("reasoning: read generation components: %w", err)
	}
	defer rows.Close()

	out.Components = []UsedComponent{}
	for rows.Next() {
		var c UsedComponent
		var content string
		if err := rows.Scan(&c.Type, &c.Position, &c.VersionNumber, &content); err != nil {
			return Reasoning{}, fmt.Errorf("reasoning: scan generation component: %w", err)
		}
		c.Content = &content
		out.Components = append(out.Components, c)
	}
	if err := rows.Err(); err != nil {
		return Reasoning{}, fmt.Errorf("reasoning: read generation components: %w", err)
	}
	return out, nil
}
