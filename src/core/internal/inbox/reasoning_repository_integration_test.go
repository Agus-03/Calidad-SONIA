package inbox

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// ADR-0049 against the real DDL: the components come in the order they were assembled, with the version that was used and not the
// current one, a component removed afterwards still shows, and an id of another conversation is not found. The thread names the
// generation behind the answer it produced.
func TestTheReasoningIsTheVersionsTheGenerationWasAssembledFrom(t *testing.T) {
	inTx(t, func(ctx context.Context, exec db.Executor) {
		w := seedWorld(t, ctx, exec)
		repo := NewRepository(exec)

		agentID := id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
			SELECT $1, a.id, 'Razona', 'autonomous', 'double', 'command-a-03-2025' FROM accounts a LIMIT 1`, agentID); err != nil {
			t.Fatalf("insert agent: %v", err)
		}
		version := func(kind enums.PromptComponentType, position, number int, content string) uuid.UUID {
			componentID, versionID := id.New(), id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO prompt_components (id, ai_agent_id, type, content, position) VALUES ($1, $2, $3, $4, $5)`,
				componentID, agentID, kind, content+" (vigente)", position); err != nil {
				t.Fatalf("insert component: %v", err)
			}
			if _, err := exec.Exec(ctx, `
				INSERT INTO prompt_component_versions (id, prompt_component_id, version_number, content, active)
				VALUES ($1, $2, $3, $4, true)`, versionID, componentID, number, content); err != nil {
				t.Fatalf("insert version: %v", err)
			}
			if kind == enums.PromptComponentTypeRules {
				// Removed after the answer: what was assembled is still what was assembled (C09).
				if _, err := exec.Exec(ctx, `UPDATE prompt_components SET deleted_at = now() WHERE id = $1`, componentID); err != nil {
					t.Fatalf("remove component: %v", err)
				}
			}
			return versionID
		}
		objective := version(enums.PromptComponentTypeObjective, 1, 3, "Alquilar inmuebles.")
		rules := version(enums.PromptComponentTypeRules, 2, 1, "No tomar pagos.")

		generationID, answerID := id.New(), id.New()
		if _, err := exec.Exec(ctx, `
			INSERT INTO messages (id, conversation_id, direction, sender_type, sender_ai_agent_id, content_text, content_type, status, sent_at)
			VALUES ($1, $2, 'outbound', 'ai_agent', $3, 'hola', 'text', 'sent', now())`, answerID, w.mineConv, agentID); err != nil {
			t.Fatalf("insert answer: %v", err)
		}
		if _, err := exec.Exec(ctx, `
			INSERT INTO generations (id, conversation_id, ai_agent_id, message_id, llm_provider, llm_model, prompt_rendered, completion_text,
			                         prompt_tokens, completion_tokens, latency_ms, cost, currency, status, assembled_at)
			VALUES ($1, $2, $3, $4, 'double', 'command-a-03-2025', 'system', 'hola', 1, 1, 1, 0, 'USD', $5, now())`,
			generationID, w.mineConv, agentID, answerID, enums.GenerationStatusAutoSent); err != nil {
			t.Fatalf("insert generation: %v", err)
		}
		// Written out of order on purpose: position is what orders them, not the order of the rows.
		for _, gc := range []struct {
			version  uuid.UUID
			position int
		}{{rules, 2}, {objective, 1}} {
			if _, err := exec.Exec(ctx, `
				INSERT INTO generation_components (id, generation_id, prompt_component_version_id, position) VALUES ($1, $2, $3, $4)`,
				id.New(), generationID, gc.version, gc.position); err != nil {
				t.Fatalf("insert generation component: %v", err)
			}
		}

		got, err := repo.Reasoning(ctx, w.mineConv, generationID)
		if err != nil {
			t.Fatalf("Reasoning: %v", err)
		}
		if len(got.Components) != 2 ||
			got.Components[0].Type != enums.PromptComponentTypeObjective || got.Components[0].VersionNumber != 3 ||
			*got.Components[0].Content != "Alquilar inmuebles." ||
			got.Components[1].Type != enums.PromptComponentTypeRules || *got.Components[1].Content != "No tomar pagos." {
			t.Fatalf("components = %+v, want objective v3 then the removed rules v1, with the text that was used", got.Components)
		}
		if _, err := repo.Reasoning(ctx, w.theirsConv, generationID); !errors.Is(err, ErrGenerationNotFound) {
			t.Fatalf("the generation through another conversation: got %v, want ErrGenerationNotFound", err)
		}

		thread, err := repo.Thread(ctx, w.mineConv, nil, 50)
		if err != nil {
			t.Fatalf("Thread: %v", err)
		}
		for _, m := range thread {
			if m.ID == answerID {
				if m.GenerationID == nil || *m.GenerationID != generationID {
					t.Fatalf("the answer names generation %v, want %s", m.GenerationID, generationID)
				}
				return
			}
		}
		t.Fatal("the answer is not in the thread")
	})
}
