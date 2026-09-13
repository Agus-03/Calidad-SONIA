package agents

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func ptr[T any](v T) *T { return &v }

func config(name string) Config {
	return Config{Name: name, LLMProvider: "cohere", LLMModel: "command-a-03-2025"}
}

// A configuration the row cannot hold honestly is refused before anything is asked or written.
func TestCreateRefusesAConfigurationNoTurnCouldRun(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Config)
		want error
	}{
		{"a blank name passes NOT NULL and names nothing", func(c *Config) { c.Name = "   " }, ErrEmptyName},
		{"an agent with no model has nothing to answer with", func(c *Config) { c.LLMModel = "" }, ErrEmptyModel},
		{"no provider accepts a negative temperature", func(c *Config) { c.Temperature = ptr(-0.1) }, ErrInvalidTemperature},
		{"zero tokens is an answer that can never be written", func(c *Config) { c.MaxTokens = ptr(0) }, ErrInvalidMaxTokens},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, catalogue := newStubRepo(), priced()
			c := config("Agente")
			tc.edit(&c)

			_, err := serviceOver(repo, &stubInboxes{}, catalogue).Create(context.Background(), testAccount, testOperator, c)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create: got %v, want %v", err, tc.want)
			}
			if len(repo.writes) != 0 || catalogue.calls != 0 {
				t.Fatalf("a refused configuration still wrote %v or asked the brain %d times", repo.writes, catalogue.calls)
			}
		})
	}
}

// The check of ADR-0038: a model the brain cannot price is refused on save, where the administrator sees it, and not on the first
// customer message, where only the log would.
func TestCreateRefusesAModelTheBrainCannotPrice(t *testing.T) {
	repo := newStubRepo()
	c := config("Agente")
	c.LLMModel = "command-r-plus"

	_, err := serviceOver(repo, &stubInboxes{}, priced()).Create(context.Background(), testAccount, testOperator, c)
	if !errors.Is(err, ErrUnpricedModel) {
		t.Fatalf("Create: got %v, want ErrUnpricedModel", err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("an unpriced agent was written: %v", repo.writes)
	}
}

// A brain that cannot answer refuses the save instead of skipping the check: an agent saved unchecked is what the check prevents.
func TestCreateRefusesWhenTheBrainCannotSayWhatIsPriced(t *testing.T) {
	repo := newStubRepo()
	catalogue := &stubCatalogue{err: errors.New("brain: unreachable")}

	_, err := serviceOver(repo, &stubInboxes{}, catalogue).Create(context.Background(), testAccount, testOperator, config("Agente"))
	if !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("Create: got %v, want ErrModelsUnavailable", err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("an agent was written without the check: %v", repo.writes)
	}
}

// Every change of configuration leaves who, what and when, in the same transaction as the change (ADR-0007).
func TestCreateAuditsTheAgentItWrote(t *testing.T) {
	repo := newStubRepo()
	c := config("  Agente de ventas  ")
	c.Description = ptr("   ")
	c.Temperature = ptr(0.3)

	created, err := serviceOver(repo, &stubInboxes{}, priced()).Create(context.Background(), testAccount, testOperator, c)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Agente de ventas" || created.Description != nil || !created.Active || created.AccountID != testAccount {
		t.Fatalf("created = %+v, want the name trimmed, a blank description stored as NULL, born active in the account", created)
	}
	if !slices.Equal(repo.writes, []string{"create", "audit"}) {
		t.Fatalf("writes = %v, want the agent and then its audit", repo.writes)
	}

	entry, ch := repo.lastChange()
	if entry.Action != actionCreate || entry.EntityType != entityAIAgent || entry.EntityID != created.ID || entry.OperatorID != testOperator {
		t.Fatalf("audit = %+v, want create of %v by %v", entry, created.ID, testOperator)
	}
	if ch.Before != nil || ch.After == nil || ch.After.Name != "Agente de ventas" || *ch.After.Temperature != 0.3 {
		t.Fatalf("changes = %+v, want no before and the configuration written as after", ch)
	}
}

// A save that changes nothing writes nothing: an audit row per click would bury the changes that matter for OE2.
func TestUpdateWithNothingNewWritesNothing(t *testing.T) {
	existing := seeded("Agente")
	repo, catalogue := newStubRepo(existing), priced()

	got, err := serviceOver(repo, &stubInboxes{}, catalogue).
		Update(context.Background(), testAccount, testOperator, existing.ID, func(c *Config) { c.Name = " Agente " })
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.ID != existing.ID || len(repo.writes) != 0 || catalogue.calls != 0 {
		t.Fatalf("a save with no change wrote %v and asked the brain %d times", repo.writes, catalogue.calls)
	}
}

// The brain is asked only when the model is what changes: renaming an agent does not depend on the other service being up.
func TestUpdateAsksTheBrainOnlyWhenTheModelChanges(t *testing.T) {
	existing := seeded("Agente")
	repo := newStubRepo(existing)
	catalogue := &stubCatalogue{err: errors.New("brain: unreachable")}

	renamed, err := serviceOver(repo, &stubInboxes{}, catalogue).
		Update(context.Background(), testAccount, testOperator, existing.ID, func(c *Config) { c.Name = "Agente nuevo" })
	if err != nil {
		t.Fatalf("a rename with the brain down: got %v, want it saved", err)
	}
	if renamed.Name != "Agente nuevo" || catalogue.calls != 0 {
		t.Fatalf("renamed = %q with %d calls to the brain, want the new name and none", renamed.Name, catalogue.calls)
	}

	_, err = serviceOver(repo, &stubInboxes{}, catalogue).
		Update(context.Background(), testAccount, testOperator, existing.ID, func(c *Config) { c.LLMModel = "claude-sonnet-4-5" })
	if !errors.Is(err, ErrModelsUnavailable) {
		t.Fatalf("a model change with the brain down: got %v, want ErrModelsUnavailable", err)
	}
}

func TestUpdateRefusesAModelTheBrainCannotPrice(t *testing.T) {
	existing := seeded("Agente")
	repo := newStubRepo(existing)

	_, err := serviceOver(repo, &stubInboxes{}, priced()).
		Update(context.Background(), testAccount, testOperator, existing.ID, func(c *Config) { c.LLMProvider = "openai" })
	if !errors.Is(err, ErrUnpricedModel) {
		t.Fatalf("Update: got %v, want ErrUnpricedModel", err)
	}
	if len(repo.writes) != 0 {
		t.Fatalf("an unpriced model was written: %v", repo.writes)
	}
}

// A pause is an edit like any other and audits its before and after, which is how OE2 learns when the agent stopped answering.
func TestPausingAnAgentIsAuditedWithItsBeforeAndAfter(t *testing.T) {
	existing := seeded("Agente")
	existing.Description = ptr("vieja")
	repo := newStubRepo(existing)

	paused, err := serviceOver(repo, &stubInboxes{}, priced()).
		Update(context.Background(), testAccount, testOperator, existing.ID, func(c *Config) {
			c.Active = false
			c.Description = nil
		})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if paused.Active || paused.Description != nil {
		t.Fatalf("paused = %+v, want inactive with the description cleared", paused)
	}

	entry, ch := repo.lastChange()
	if entry.Action != actionUpdate || ch.Before == nil || ch.After == nil {
		t.Fatalf("audit = %+v %+v, want an update with both sides", entry, ch)
	}
	if !ch.Before.Active || ch.After.Active || *ch.Before.Description != "vieja" || ch.After.Description != nil {
		t.Fatalf("changes = before %+v after %+v, want active and the description before, neither after", *ch.Before, *ch.After)
	}
}

// Another account's agent is missing and not forbidden, and so is a removed one (C09).
func TestAnAgentOutOfReachIsNotFound(t *testing.T) {
	foreign := seeded("Ajeno")
	foreign.AccountID = id.New()
	removed := seeded("Borrado")
	repo := newStubRepo(foreign, removed)
	repo.removed[removed.ID] = true
	service := serviceOver(repo, &stubInboxes{}, priced())

	for _, agentID := range []uuid.UUID{foreign.ID, removed.ID, id.New()} {
		if _, err := service.Get(context.Background(), testAccount, agentID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get %v: got %v, want ErrNotFound", agentID, err)
		}
		if _, err := service.Update(context.Background(), testAccount, testOperator, agentID, func(*Config) {}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update %v: got %v, want ErrNotFound", agentID, err)
		}
		if err := service.Delete(context.Background(), testAccount, testOperator, agentID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete %v: got %v, want ErrNotFound", agentID, err)
		}
	}
	if len(repo.writes) != 0 {
		t.Fatalf("an agent out of reach was written: %v", repo.writes)
	}
}

// The removal clears the inboxes that default to the agent in the same transaction, and the audit is the one place that says which
// (ADR-0038).
func TestRemovingAnAgentClearsItsInboxesAndSaysWhich(t *testing.T) {
	existing := seeded("Agente")
	repo := newStubRepo(existing)
	inboxes := &stubInboxes{cleared: []uuid.UUID{id.New(), id.New()}}

	if err := serviceOver(repo, inboxes, priced()).Delete(context.Background(), testAccount, testOperator, existing.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !slices.Equal(inboxes.asked, []uuid.UUID{existing.ID}) {
		t.Fatalf("inboxes cleared for %v, want exactly %v", inboxes.asked, existing.ID)
	}
	if !slices.Equal(repo.writes, []string{"delete", "audit"}) {
		t.Fatalf("writes = %v, want the removal and then its audit", repo.writes)
	}

	entry, ch := repo.lastChange()
	if entry.Action != actionDelete || entry.EntityID != existing.ID {
		t.Fatalf("audit = %+v, want delete of %v", entry, existing.ID)
	}
	if ch.Before == nil || ch.After != nil || !slices.Equal(ch.ClearedInboxIDs, inboxes.cleared) {
		t.Fatalf("changes = %+v, want the configuration before, nothing after and the cleared inboxes %v", ch, inboxes.cleared)
	}

	if list, _ := serviceOver(repo, inboxes, priced()).List(context.Background(), testAccount); len(list) != 0 {
		t.Fatalf("the listing still shows %d agents after the removal", len(list))
	}
}

// A write that fails stops the transaction before its audit, so no row describes a change that never committed.
func TestAFailedWriteLeavesNoAudit(t *testing.T) {
	repo := newStubRepo()
	repo.err = errors.New("postgres: gone")

	if _, err := serviceOver(repo, &stubInboxes{}, priced()).Create(context.Background(), testAccount, testOperator, config("Agente")); err == nil {
		t.Fatal("Create: got nil, want the failure of the write")
	}
	if len(repo.audits) != 0 {
		t.Fatalf("an audit row was written for a change that failed: %+v", repo.audits)
	}
}
