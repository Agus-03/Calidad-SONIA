package whatsapp

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestOneLiveInboxPerNumber protects the index behind InboxByPhoneNumberID: with two live rows for one number the webhook would
// resolve either of them, and check the signature against either secret (ADR-0012, relaciones.md section 9).
func TestOneLiveInboxPerNumber(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		phoneNumberID := "it-" + id.New().String()
		insert := func() error {
			_, err := exec.Exec(ctx, `
				INSERT INTO inboxes (id, account_id, phone_number_id, waba_id, display_name, verify_token, status)
				VALUES ($1, $2, $3, 'it-waba', 'Numero repetido', $4, 'active')`,
				id.New(), account, phoneNumberID, "it-"+id.New().String())
			return err
		}

		if err := insert(); err != nil {
			t.Fatalf("the first inbox of the number: %v", err)
		}
		// Retiring the row frees the number, the same partial index the other three uniques of section 9 use (C09).
		if _, err := exec.Exec(ctx, `UPDATE inboxes SET deleted_at = now() WHERE phone_number_id = $1`, phoneNumberID); err != nil {
			t.Fatalf("retire the first inbox: %v", err)
		}
		if err := insert(); err != nil {
			t.Fatalf("the number after its inbox was retired: got %v, want it free again", err)
		}

		// Last on purpose: the refused statement aborts the transaction, so nothing can run after it (SQLSTATE 25P02).
		var pgErr *pgconn.PgError
		if err := insert(); !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			t.Fatalf("a second live inbox for the number: got %v, want a unique violation", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestRemovingAnAIAgentClearsTheInboxesThatDefaultedToIt is the half of the removal whatsapp/ owns: after it, no new conversation
// is born with the removed agent, and the inboxes that pointed elsewhere keep their own default (ADR-0038).
func TestRemovingAnAIAgentClearsTheInboxesThatDefaultedToIt(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		agent := func() uuid.UUID {
			agentID := id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
				VALUES ($1, $2, 'Agente de integracion', 'autonomous', 'cohere', 'command-a-03-2025')`,
				agentID, account); err != nil {
				t.Fatalf("seed an agent: %v", err)
			}
			return agentID
		}
		inbox := func(agentID uuid.UUID) uuid.UUID {
			inboxID := id.New()
			if _, err := exec.Exec(ctx, `
				INSERT INTO inboxes (id, account_id, default_ai_agent_id, phone_number_id, waba_id, display_name, verify_token, status)
				VALUES ($1, $2, $3, $4, 'it-waba', 'Numero de integracion', $5, 'active')`,
				inboxID, account, agentID, "it-"+id.New().String(), "it-"+id.New().String()); err != nil {
				t.Fatalf("seed an inbox: %v", err)
			}
			return inboxID
		}

		removed, kept := agent(), agent()
		first, second, other := inbox(removed), inbox(removed), inbox(kept)

		cleared, err := NewRepository(exec, testCipher(t)).ClearDefaultAIAgent(ctx, account, removed)
		if err != nil {
			t.Fatalf("ClearDefaultAIAgent: %v", err)
		}
		if len(cleared) != 2 || !slices.Contains(cleared, first) || !slices.Contains(cleared, second) {
			t.Fatalf("cleared = %v, want exactly %v and %v", cleared, first, second)
		}

		var defaults []*uuid.UUID
		for _, inboxID := range []uuid.UUID{first, second, other} {
			var agentID *uuid.UUID
			if err := exec.QueryRow(ctx, `SELECT default_ai_agent_id FROM inboxes WHERE id = $1`, inboxID).Scan(&agentID); err != nil {
				t.Fatalf("read the default of %v: %v", inboxID, err)
			}
			defaults = append(defaults, agentID)
		}
		if defaults[0] != nil || defaults[1] != nil {
			t.Fatalf("the inboxes of the removed agent still default to %v and %v, want NULL", defaults[0], defaults[1])
		}
		if defaults[2] == nil || *defaults[2] != kept {
			t.Fatalf("the inbox of another agent defaults to %v, want it untouched on %v", defaults[2], kept)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
