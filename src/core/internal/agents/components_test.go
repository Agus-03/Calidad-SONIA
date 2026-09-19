package agents

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubComponents answers from memory and plays both halves of the repository, so the service is tested against its own rules.
type stubComponents struct {
	agents     map[uuid.UUID]AIAgent
	components []Component
	removed    map[uuid.UUID]bool
	versions   []ComponentVersion
	// Every write in the order it happened, which is how a case sees that a save with no change wrote nothing.
	writes []string
	err    error
}

func newStubComponents(agents ...AIAgent) *stubComponents {
	s := &stubComponents{agents: map[uuid.UUID]AIAgent{}, removed: map[uuid.UUID]bool{}}
	for _, a := range agents {
		s.agents[a.ID] = a
	}
	return s
}

func (s *stubComponents) ByID(_ context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	a, ok := s.agents[agentID]
	if !ok || a.AccountID != accountID {
		return AIAgent{}, ErrNotFound
	}
	return a, nil
}

func (s *stubComponents) ByIDForUpdate(ctx context.Context, accountID, agentID uuid.UUID) (AIAgent, error) {
	return s.ByID(ctx, accountID, agentID)
}

func (s *stubComponents) find(accountID, agentID, componentID uuid.UUID) (int, error) {
	if _, err := s.ByID(context.Background(), accountID, agentID); err != nil {
		return 0, ErrComponentNotFound
	}
	for i, c := range s.components {
		if c.ID == componentID && c.AIAgentID == agentID && !s.removed[c.ID] {
			return i, nil
		}
	}
	return 0, ErrComponentNotFound
}

func (s *stubComponents) Components(_ context.Context, _, agentID uuid.UUID) ([]Component, error) {
	out := []Component{}
	for _, c := range s.components {
		if c.AIAgentID == agentID && !s.removed[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *stubComponents) Component(_ context.Context, accountID, agentID, componentID uuid.UUID) (Component, error) {
	i, err := s.find(accountID, agentID, componentID)
	if err != nil {
		return Component{}, err
	}
	return s.components[i], nil
}

func (s *stubComponents) ComponentForUpdate(ctx context.Context, accountID, agentID, componentID uuid.UUID) (Component, error) {
	return s.Component(ctx, accountID, agentID, componentID)
}

func (s *stubComponents) ComponentTaken(_ context.Context, agentID uuid.UUID, t enums.PromptComponentType, position int16) (bool, bool, error) {
	var typeTaken, positionTaken bool
	for _, c := range s.components {
		if c.AIAgentID == agentID && !s.removed[c.ID] {
			typeTaken = typeTaken || c.Type == t
			positionTaken = positionTaken || c.Position == position
		}
	}
	return typeTaken, positionTaken, nil
}

func (s *stubComponents) CreateComponent(_ context.Context, c Component) (Component, error) {
	if s.err != nil {
		return Component{}, s.err
	}
	c.Active, c.CreatedAt, c.UpdatedAt = true, time.Now().UTC(), time.Now().UTC()
	s.components = append(s.components, c)
	s.writes = append(s.writes, "create")
	return c, nil
}

func (s *stubComponents) UpdateComponent(_ context.Context, componentID uuid.UUID, content string, active bool) (Component, error) {
	for i := range s.components {
		if s.components[i].ID == componentID {
			s.components[i].Content, s.components[i].Active = content, active
			s.writes = append(s.writes, "update")
			return s.components[i], nil
		}
	}
	return Component{}, ErrComponentNotFound
}

func (s *stubComponents) SoftDeleteComponent(_ context.Context, componentID uuid.UUID) error {
	s.removed[componentID] = true
	s.writes = append(s.writes, "delete")
	return nil
}

func (s *stubComponents) AppendVersion(_ context.Context, v ComponentVersion) (int, error) {
	v.Number = 1
	for _, earlier := range s.versions {
		if earlier.ComponentID == v.ComponentID && earlier.Number >= v.Number {
			v.Number = earlier.Number + 1
		}
	}
	v.CreatedAt = time.Now().UTC()
	s.versions = append(s.versions, v)
	s.writes = append(s.writes, "version")
	for i := range s.components {
		if s.components[i].ID == v.ComponentID {
			s.components[i].Version = v.Number
		}
	}
	return v.Number, nil
}

func (s *stubComponents) Versions(_ context.Context, componentID uuid.UUID) ([]ComponentVersion, error) {
	out := []ComponentVersion{}
	for i := len(s.versions) - 1; i >= 0; i-- {
		if s.versions[i].ComponentID == componentID {
			out = append(out, s.versions[i])
		}
	}
	return out, nil
}

// The transaction is a plain call here: what the test checks is which writes happen, and binding them is the pool's job.
func componentsOver(store *stubComponents) *Components {
	return NewComponents(ComponentOptions{
		Repo: store,
		Tx:   func(_ context.Context, fn func(ComponentStore) error) error { return fn(store) },
	})
}

func tone(content string) NewComponent {
	return NewComponent{Type: enums.PromptComponentTypeTone, Content: content, Position: 3}
}

// A block the prompt cannot hold honestly is refused before anything is written.
func TestAComponentNoPromptCouldAssembleIsRefused(t *testing.T) {
	agent := seeded("Agente")
	for _, tc := range []struct {
		name string
		edit func(*NewComponent)
		want error
	}{
		{"a type F01 does not declare", func(n *NewComponent) { n.Type = "greeting" }, ErrUnknownComponentType},
		{"a blank block prints a heading with nothing under it", func(n *NewComponent) { n.Content = "   " }, ErrEmptyContent},
		{"position zero is before the first", func(n *NewComponent) { n.Position = 0 }, ErrInvalidPosition},
		{"a position the smallint column cannot hold", func(n *NewComponent) { n.Position = 40000 }, ErrInvalidPosition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStubComponents(agent)
			n := tone("Formal pero cálido.")
			tc.edit(&n)

			if _, err := componentsOver(store).Create(context.Background(), testAccount, testOperator, agent.ID, n); !errors.Is(err, tc.want) {
				t.Fatalf("Create: got %v, want %v", err, tc.want)
			}
			if len(store.writes) != 0 {
				t.Fatalf("a refused block still wrote %v", store.writes)
			}
		})
	}
}

// The first version is written with the block, by who wrote it: a block without its version is a text OE2 cannot date (H-20).
func TestAComponentIsBornWithItsFirstVersion(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)
	n := tone("  Formal pero cálido.  ")
	n.ChangeNote = ptr("   ")

	created, err := componentsOver(store).Create(context.Background(), testAccount, testOperator, agent.ID, n)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Content != "Formal pero cálido." || !created.Active || created.Version != 1 || created.Position != 3 {
		t.Fatalf("created = %+v, want the text trimmed, born active, at version 1 and position 3", created)
	}
	if !slices.Equal(store.writes, []string{"create", "version"}) {
		t.Fatalf("writes = %v, want the block and then its version", store.writes)
	}
	v := store.versions[0]
	if v.Content != created.Content || !v.Active || v.ChangedBy == nil || *v.ChangedBy != testOperator || v.ChangeNote != nil {
		t.Fatalf("version = %+v, want the same text, active, by %v and a blank note stored as none", v, testOperator)
	}
}

// One block per type: the panel writes one tone, and changing it is a new version of the same block (ADR-0039). And two blocks at
// one position would leave their order in the prompt to chance.
func TestAnAIAgentHasOneComponentOfEachTypeAndOneAtEachPosition(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)
	service := componentsOver(store)
	if _, err := service.Create(context.Background(), testAccount, testOperator, agent.ID, tone("Formal.")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	second := tone("Informal.")
	second.Position = 4
	if _, err := service.Create(context.Background(), testAccount, testOperator, agent.ID, second); !errors.Is(err, ErrComponentTypeTaken) {
		t.Fatalf("a second tone: got %v, want ErrComponentTypeTaken", err)
	}
	rules := NewComponent{Type: enums.PromptComponentTypeRules, Content: "No tomar pagos.", Position: 3}
	if _, err := service.Create(context.Background(), testAccount, testOperator, agent.ID, rules); !errors.Is(err, ErrComponentPositionTaken) {
		t.Fatalf("a block at a taken position: got %v, want ErrComponentPositionTaken", err)
	}
	if _, err := service.Create(context.Background(), testAccount, testOperator, id.New(), rules); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a block of an agent out of reach: got %v, want ErrNotFound", err)
	}
}

// Regla 2 of 03-capa-prompts: a new version when the text or the switch changed, and nothing written when neither did.
func TestAComponentIsVersionedWhenItsTextOrItsSwitchChanges(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)
	service := componentsOver(store)
	created, err := service.Create(context.Background(), testAccount, testOperator, agent.ID, tone("Formal."))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	update := func(e ComponentEdit) Component {
		t.Helper()
		got, err := service.Update(context.Background(), testAccount, testOperator, agent.ID, created.ID, e)
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		return got
	}

	if got := update(ComponentEdit{Content: ptr(" Formal. "), Active: ptr(true)}); got.Version != 1 || len(store.versions) != 1 {
		t.Fatalf("a save with no change is at version %d with %d versions, want 1 and 1", got.Version, len(store.versions))
	}
	if got := update(ComponentEdit{Content: ptr("Cálido."), ChangeNote: ptr("más cercano")}); got.Version != 2 || got.Content != "Cálido." {
		t.Fatalf("a new text: got %+v, want version 2 with the new text", got)
	}
	if got := update(ComponentEdit{Active: ptr(false)}); got.Version != 3 || got.Active {
		t.Fatalf("switching it off: got %+v, want version 3 and off, the ablation OE1 reads (H-20)", got)
	}

	last := store.versions[len(store.versions)-1]
	if last.Active || last.Content != "Cálido." || store.versions[1].ChangeNote == nil || *store.versions[1].ChangeNote != "más cercano" {
		t.Fatalf("versions = %+v, want the switch frozen off with the text kept, and the note of the edit", store.versions)
	}
	if _, err := service.Update(context.Background(), testAccount, testOperator, agent.ID, created.ID, ComponentEdit{Content: ptr(" ")}); !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("a blank text: got %v, want ErrEmptyContent", err)
	}
	if _, err := service.Update(context.Background(), testAccount, testOperator, agent.ID, id.New(), ComponentEdit{Active: ptr(true)}); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("a block that does not exist: got %v, want ErrComponentNotFound", err)
	}
}

// The removal closes the block with a last version, off and naming who removed it, since the row has nowhere to say so (C09).
func TestRemovingAComponentLeavesWhoRemovedIt(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)
	service := componentsOver(store)
	created, err := service.Create(context.Background(), testAccount, testOperator, agent.ID, tone("Formal."))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	remover := id.New()

	if err := service.Delete(context.Background(), testAccount, remover, agent.ID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	last := store.versions[len(store.versions)-1]
	if last.Active || last.Number != 2 || last.ChangedBy == nil || *last.ChangedBy != remover {
		t.Fatalf("last version = %+v, want version 2, off, by %v", last, remover)
	}
	if list, _ := service.List(context.Background(), testAccount, agent.ID); len(list) != 0 {
		t.Fatalf("the listing still shows %d blocks after the removal", len(list))
	}
	if _, err := service.Versions(context.Background(), testAccount, agent.ID, created.ID); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("the history of a removed block: got %v, want ErrComponentNotFound", err)
	}
	if err := service.Delete(context.Background(), testAccount, remover, agent.ID, created.ID); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("removing twice: got %v, want ErrComponentNotFound", err)
	}
	if _, err := service.List(context.Background(), testAccount, id.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the blocks of an agent out of reach: got %v, want ErrNotFound and not an empty list", err)
	}
}

// A write that fails stops the transaction before the version, so no version describes a block that never committed.
func TestAFailedComponentWriteLeavesNoVersion(t *testing.T) {
	agent := seeded("Agente")
	store := newStubComponents(agent)
	store.err = errors.New("postgres: gone")

	if _, err := componentsOver(store).Create(context.Background(), testAccount, testOperator, agent.ID, tone("Formal.")); err == nil {
		t.Fatal("Create: got nil, want the failure of the write")
	}
	if len(store.versions) != 0 {
		t.Fatalf("a version was written for a block that failed: %+v", store.versions)
	}
}
