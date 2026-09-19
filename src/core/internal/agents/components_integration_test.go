package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The life of one block against the real DDL, through the real repository and in the case's own transaction: every change is a
// version numbered after the last, the text the brain reads is always the one of the newest version, and a removal leaves the type
// free again (ADR-0039, 03-capa-prompts Regla 2).
func TestAComponentLivesThroughItsVersionsAgainstRealDDL(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor, account uuid.UUID) {
		operator := seededOperator(t, ctx, exec, account)
		repo := NewRepository(exec)
		agent, err := repo.Create(ctx, AIAgent{ID: id.New(), AccountID: account, Name: "Agente con componentes",
			LLMProvider: "cohere", LLMModel: "command-a-03-2025"})
		if err != nil {
			t.Fatalf("Create the agent: %v", err)
		}
		service := NewComponents(ComponentOptions{
			Repo: repo,
			// The case's own transaction plays the one deps.go opens, so the rollback undoes all of it.
			Tx: func(_ context.Context, fn func(ComponentStore) error) error { return fn(repo) },
		})

		// What the brain reads to assemble the prompt (src/brain/prompts/repository.py): the active blocks by position.
		assembled := func() []string {
			t.Helper()
			rows, err := exec.Query(ctx, `
				SELECT c.content FROM prompt_components c
				WHERE  c.ai_agent_id = $1 AND c.active AND c.deleted_at IS NULL ORDER BY c.position`, agent.ID)
			if err != nil {
				t.Fatalf("read as the brain: %v", err)
			}
			defer rows.Close()
			out := []string{}
			for rows.Next() {
				var content string
				if err := rows.Scan(&content); err != nil {
					t.Fatalf("scan: %v", err)
				}
				out = append(out, content)
			}
			return out
		}

		created, err := service.Create(ctx, account, operator, agent.ID, NewComponent{
			Type: enums.PromptComponentTypeTone, Content: "Formal.", Position: 3, ChangeNote: ptr("primera versión"),
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if created.Version != 1 {
			t.Fatalf("created at version %d, want 1", created.Version)
		}
		if _, err := service.Create(ctx, account, operator, agent.ID, NewComponent{
			Type: enums.PromptComponentTypeTone, Content: "Otro.", Position: 4,
		}); !errors.Is(err, ErrComponentTypeTaken) {
			t.Fatalf("a second tone: got %v, want ErrComponentTypeTaken", err)
		}

		edited, err := service.Update(ctx, account, operator, agent.ID, created.ID, ComponentEdit{Content: ptr("Cálido.")})
		if err != nil || edited.Version != 2 {
			t.Fatalf("an edit: got %+v, %v, want version 2", edited, err)
		}
		if got := assembled(); len(got) != 1 || got[0] != "Cálido." {
			t.Fatalf("the brain reads %v, want the text of the newest version", got)
		}
		if off, err := service.Update(ctx, account, operator, agent.ID, created.ID, ComponentEdit{Active: ptr(false)}); err != nil || off.Version != 3 {
			t.Fatalf("switching it off: got %+v, %v, want version 3", off, err)
		}
		if got := assembled(); len(got) != 0 {
			t.Fatalf("the brain still reads %v with the block off", got)
		}
		// A save with no change is read back through the real lock and writes no version (Regla 2).
		if same, err := service.Update(ctx, account, operator, agent.ID, created.ID, ComponentEdit{Active: ptr(false)}); err != nil || same.Version != 3 {
			t.Fatalf("a save with no change: got %+v, %v, want still version 3", same, err)
		}

		history, err := service.Versions(ctx, account, agent.ID, created.ID)
		if err != nil || len(history) != 3 {
			t.Fatalf("Versions: got %d, %v, want 3", len(history), err)
		}
		first := history[2]
		if history[0].Number != 3 || history[0].Active || first.Content != "Formal." || first.ChangeNote == nil ||
			first.ChangedBy == nil || *first.ChangedBy != operator {
			t.Fatalf("history = %+v, want newest first, the switch frozen off and the first version by %v with its note", history, operator)
		}

		if err := service.Delete(ctx, account, operator, agent.ID, created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		var versions int
		var lastActive bool
		if err := exec.QueryRow(ctx, `
			SELECT count(*), (array_agg(active ORDER BY version_number DESC))[1]
			FROM   prompt_component_versions WHERE prompt_component_id = $1`, created.ID).Scan(&versions, &lastActive); err != nil {
			t.Fatalf("read the versions: %v", err)
		}
		if versions != 4 || lastActive {
			t.Fatalf("after the removal: %d versions, the last active %v; want 4 and off", versions, lastActive)
		}
		if _, err := service.Create(ctx, account, operator, agent.ID, NewComponent{
			Type: enums.PromptComponentTypeTone, Content: "Nuevo tono.", Position: 3,
		}); err != nil {
			t.Fatalf("a tone after the removal of the last one: %v", err)
		}

		// The blocks of a removed agent are out of reach like the agent (C09).
		if err := repo.SoftDelete(ctx, account, agent.ID); err != nil {
			t.Fatalf("remove the agent: %v", err)
		}
		if list, err := repo.Components(ctx, account, agent.ID); err != nil || len(list) != 0 {
			t.Fatalf("the blocks of a removed agent: got %d, %v, want none", len(list), err)
		}
	})
}
