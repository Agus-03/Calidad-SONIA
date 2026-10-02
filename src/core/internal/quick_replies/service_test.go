package quickreplies

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL that enforces them.
type stubStore struct {
	rows map[uuid.UUID]QuickReply
	// Set to make a read fail, which is how a test reaches the branch that lets an unknown error through as a 500.
	err error
}

func newStore(rows ...QuickReply) *stubStore {
	s := &stubStore{rows: map[uuid.UUID]QuickReply{}}
	for _, q := range rows {
		s.rows[q.ID] = q
	}
	return s
}

// Folded the same way the functional index folds it, because a stub stricter than the database proves nothing.
func (s *stubStore) taken(accountID uuid.UUID, title string, except uuid.UUID) bool {
	for _, q := range s.rows {
		if q.AccountID == accountID && q.ID != except && strings.EqualFold(q.Title, normalizeTitle(title)) {
			return true
		}
	}
	return false
}

func (s *stubStore) List(_ context.Context, accountID uuid.UUID, text string, onlyActive bool, limit, offset int) (Page, error) {
	if s.err != nil {
		return Page{}, s.err
	}
	found := []QuickReply{}
	for _, q := range s.rows {
		hit := strings.Contains(strings.ToLower(q.Title), strings.ToLower(text)) || strings.Contains(strings.ToLower(q.Content), strings.ToLower(text))
		if q.AccountID == accountID && hit && (q.Active || !onlyActive) {
			found = append(found, q)
		}
	}
	sort.Slice(found, func(i, j int) bool { return strings.ToLower(found[i].Title) < strings.ToLower(found[j].Title) })
	page := Page{Items: []QuickReply{}, Total: len(found)}
	for i := offset; i < len(found) && i < offset+limit; i++ {
		page.Items = append(page.Items, found[i])
	}
	return page, nil
}

func (s *stubStore) ByID(_ context.Context, accountID, replyID uuid.UUID) (QuickReply, error) {
	q, ok := s.rows[replyID]
	if !ok || q.AccountID != accountID {
		return QuickReply{}, ErrNotFound
	}
	return q, nil
}

func (s *stubStore) Create(_ context.Context, q QuickReply) error {
	if s.taken(q.AccountID, q.Title, q.ID) {
		return ErrDuplicateTitle
	}
	s.rows[q.ID] = q
	return nil
}

func (s *stubStore) Update(_ context.Context, accountID, replyID uuid.UUID, title, content *string, active *bool) error {
	q, ok := s.rows[replyID]
	if !ok || q.AccountID != accountID {
		return ErrNotFound
	}
	if title != nil {
		if s.taken(accountID, *title, replyID) {
			return ErrDuplicateTitle
		}
		q.Title = normalizeTitle(*title)
	}
	if content != nil {
		q.Content = *content
	}
	if active != nil {
		q.Active = *active
	}
	s.rows[replyID] = q
	return nil
}

func (s *stubStore) SoftDelete(_ context.Context, accountID, replyID uuid.UUID) error {
	if q, ok := s.rows[replyID]; !ok || q.AccountID != accountID {
		return ErrNotFound
	}
	delete(s.rows, replyID)
	return nil
}

func scopeOf(accountID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: accountID, OperatorID: id.New(), Role: role}
}

func reply(accountID uuid.UUID, title string, active bool) QuickReply {
	return QuickReply{ID: id.New(), AccountID: accountID, Title: title, Content: "texto de " + title, Active: active, CreatedByOperatorID: id.New()}
}

// TestOnlyTheAdminSeesThePausedReplies is ADR-0060: a paused reply leaves the chat and stays in the admin's list to be resumed.
func TestOnlyTheAdminSeesThePausedReplies(t *testing.T) {
	account := id.New()
	store := newStore(reply(account, "Horarios", true), reply(account, "Promo vieja", false), reply(id.New(), "Otra cuenta", true))

	for _, tc := range []struct {
		role enums.OperatorRole
		want int
	}{
		{enums.OperatorRoleOperator, 1},
		{enums.OperatorRoleSupervisor, 1},
		{enums.OperatorRoleAdmin, 2},
	} {
		page, err := NewService(store, nil).List(context.Background(), scopeOf(account, tc.role), "", 20, 0)
		if err != nil || page.Total != tc.want || len(page.Items) != tc.want {
			t.Fatalf("%s: got %d of %d and %v, want %d", tc.role, len(page.Items), page.Total, err, tc.want)
		}
	}
}

// TestTheSearchLooksAtTheTitleAndTheContent is how an operator finds a reply while typing in the chat (F09).
func TestTheSearchLooksAtTheTitleAndTheContent(t *testing.T) {
	account := id.New()
	hours, prices := reply(account, "Horarios", true), reply(account, "Precios", true)
	prices.Content = "La lista de precios con el HORARIO de entrega"
	store := newStore(hours, prices, reply(account, "Envíos", true))

	page, err := NewService(store, nil).List(context.Background(), scopeOf(account, enums.OperatorRoleOperator), "  horario ", 20, 0)
	if err != nil || page.Total != 2 || page.Items[0].ID != hours.ID || page.Items[1].ID != prices.ID {
		t.Fatalf("got %+v and %v, want Horarios then Precios", page, err)
	}
}

// TestATitleIsTakenOncePerAccount is the decision of the group: two replies called "Horarios" in any casing are refused (ADR-0060).
func TestATitleIsTakenOncePerAccount(t *testing.T) {
	account := id.New()
	existing, other := reply(account, "Horarios", true), reply(account, "Precios", true)
	store := newStore(existing, other)
	service := NewService(store, nil)
	admin := scopeOf(account, enums.OperatorRoleAdmin)

	if _, err := service.Create(context.Background(), admin, "  horarios ", "otro texto"); !errors.Is(err, ErrDuplicateTitle) {
		t.Fatalf("Create: got %v, want ErrDuplicateTitle", err)
	}
	if _, err := service.Create(context.Background(), scopeOf(id.New(), enums.OperatorRoleAdmin), "Horarios", "otro texto"); err != nil {
		t.Fatalf("the same title in another account: %v", err)
	}
	renamed := "HORARIOS"
	if _, err := service.Update(context.Background(), admin, other.ID, &renamed, nil, nil); !errors.Is(err, ErrDuplicateTitle) {
		t.Fatalf("Update: got %v, want ErrDuplicateTitle", err)
	}
}

// TestTheAdminLoadsEditsPausesAndDeletes walks the life of a reply, with the author taken from who asks and never from the body.
func TestTheAdminLoadsEditsPausesAndDeletes(t *testing.T) {
	account := id.New()
	store := newStore()
	service := NewService(store, nil)
	admin := scopeOf(account, enums.OperatorRoleAdmin)
	ctx := context.Background()

	created, err := service.Create(ctx, admin, "  Horarios ", "Atendemos de 9 a 18.\nSábados de 9 a 13.")
	if err != nil || created.Title != "Horarios" || !created.Active || created.CreatedByOperatorID != admin.OperatorID ||
		created.Content != "Atendemos de 9 a 18.\nSábados de 9 a 13." {
		t.Fatalf("Create: got %+v and %v, want it active, trimmed, by the admin, with its line breaks", created, err)
	}

	paused := false
	updated, err := service.Update(ctx, admin, created.ID, nil, nil, &paused)
	if err != nil || updated.Active || updated.Title != "Horarios" || updated.Content != created.Content {
		t.Fatalf("pausing: got %+v and %v, want only active changed", updated, err)
	}

	if err := service.Delete(ctx, admin, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := service.Delete(ctx, admin, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: got %v, want ErrNotFound", err)
	}
	if _, err := service.Update(ctx, scopeOf(id.New(), enums.OperatorRoleAdmin), created.ID, nil, nil, &paused); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account's: got %v, want ErrNotFound", err)
	}
}

// TestABlankReplyIsRefused because blanks pass the NOT NULL and give a reply nobody can pick or send.
func TestABlankReplyIsRefused(t *testing.T) {
	service := NewService(newStore(), nil)
	admin := scopeOf(id.New(), enums.OperatorRoleAdmin)
	blank := "   "

	for _, tc := range []struct {
		name           string
		title, content string
		want           error
	}{
		{"a blank title", " ", "texto", ErrEmptyTitle},
		{"a blank content", "Título", "\n\t", ErrEmptyContent},
	} {
		if _, err := service.Create(context.Background(), admin, tc.title, tc.content); !errors.Is(err, tc.want) {
			t.Fatalf("Create with %s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := service.Update(context.Background(), admin, id.New(), nil, &blank, nil); !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("Update with a blank content: got %v, want ErrEmptyContent", err)
	}
}
