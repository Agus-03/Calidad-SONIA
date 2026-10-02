package sales

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL.
type stubStore struct {
	conversations map[uuid.UUID]Conversation
	rows          map[uuid.UUID]Sale
}

func newStore() *stubStore {
	return &stubStore{conversations: map[uuid.UUID]Conversation{}, rows: map[uuid.UUID]Sale{}}
}

func (s *stubStore) Conversation(_ context.Context, _, conversationID uuid.UUID) (Conversation, error) {
	c, ok := s.conversations[conversationID]
	if !ok {
		return Conversation{}, ErrConversationNotFound
	}
	return c, nil
}

// Reachable imitates the rule inbox/ owns: the holder or the pool for an operator, any for the roles above (ADR-0035, regla 2).
func (s *stubStore) Reachable(_ context.Context, scope inbox.Scope, conversationID uuid.UUID) error {
	c, ok := s.conversations[conversationID]
	if !ok || (scope.Role == enums.OperatorRoleOperator && c.OperatorID != nil && *c.OperatorID != scope.OperatorID) {
		return inbox.ErrNotFound
	}
	return nil
}

func (s *stubStore) ByID(_ context.Context, _, saleID uuid.UUID) (Sale, error) {
	sale, ok := s.rows[saleID]
	if !ok {
		return Sale{}, ErrNotFound
	}
	return sale, nil
}

func (s *stubStore) OfConversation(_ context.Context, _, conversationID uuid.UUID) ([]Sale, error) {
	out := []Sale{}
	for _, sale := range s.rows {
		if sale.ConversationID == conversationID {
			out = append(out, sale)
		}
	}
	return out, nil
}

func (s *stubStore) Create(_ context.Context, sale Sale) error {
	sale.SourceChannel = enums.SourceChannelDirect
	s.rows[sale.ID] = sale
	return nil
}

func (s *stubStore) Update(_ context.Context, saleID uuid.UUID, amount, currency string) error {
	sale, ok := s.rows[saleID]
	if !ok {
		return ErrNotFound
	}
	sale.Amount, sale.Currency = amount, currency
	s.rows[saleID] = sale
	return nil
}

func (s *stubStore) Delete(_ context.Context, saleID uuid.UUID) error {
	if _, ok := s.rows[saleID]; !ok {
		return ErrNotFound
	}
	delete(s.rows, saleID)
	return nil
}

func scopeOf(operatorID uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: id.New(), OperatorID: operatorID, Role: role}
}

// TestTheSaleIsAlwaysTheVendors is ADR-0059 as a table: who may mark, and that the vendor is credited whoever marks it.
func TestTheSaleIsAlwaysTheVendors(t *testing.T) {
	me, vendor, agent := id.New(), id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		wantErr error
	}{
		{"the vendor marks its own", &me, enums.OperatorRoleOperator, nil},
		{"a supervisor marks a vendor's, credited to the vendor", &vendor, enums.OperatorRoleSupervisor, nil},
		{"an admin marks a vendor's, credited to the vendor", &vendor, enums.OperatorRoleAdmin, nil},
		{"another's does not exist for an operator", &vendor, enums.OperatorRoleOperator, ErrConversationNotFound},
		{"one of the pool has nobody to credit", nil, enums.OperatorRoleAdmin, ErrNotAssigned},
		{"an operator on one of the pool is told the same", nil, enums.OperatorRoleOperator, ErrNotAssigned},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore()
			conversation := Conversation{ID: id.New(), OperatorID: tc.holder, AIAgentID: &agent}
			store.conversations[conversation.ID] = conversation

			sale, err := NewService(store, store, nil).Mark(context.Background(), scopeOf(me, tc.role), conversation.ID, "1234.50", "")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Mark: got %v, want %v", err, tc.wantErr)
			}
			if (len(store.rows) == 1) != (tc.wantErr == nil) {
				t.Fatalf("%d sales written, want one only when allowed", len(store.rows))
			}
			if tc.wantErr == nil && (sale.OperatorID != *tc.holder || sale.AIAgentID == nil || *sale.AIAgentID != agent ||
				sale.Amount != "1234.50" || sale.Currency != defaultCurrency) {
				t.Fatalf("the sale %+v is not credited to the vendor with the agent, the amount and ARS", sale)
			}
		})
	}
}

// TestTheAmountAndTheCurrencyAreWhatTheSchemaKeeps: numeric(14,2) and an ISO code, refused before anything is written (C08).
func TestTheAmountAndTheCurrencyAreWhatTheSchemaKeeps(t *testing.T) {
	me := id.New()
	for _, tc := range []struct {
		amount, currency string
		wantErr          error
	}{
		{"1", "USD", nil},
		{"999999999999.99", "", nil},
		{"0.5", "ARS", nil},
		{"0", "", ErrInvalidAmount},
		{"0.00", "", ErrInvalidAmount},
		{"-10", "", ErrInvalidAmount},
		{"10.555", "", ErrInvalidAmount},
		{"1e3", "", ErrInvalidAmount},
		{"1000000000000", "", ErrInvalidAmount},
		{"10,50", "", ErrInvalidAmount},
		{"10", "usd", ErrInvalidCurrency},
		{"10", "PESOS", ErrInvalidCurrency},
	} {
		store := newStore()
		conversation := Conversation{ID: id.New(), OperatorID: &me}
		store.conversations[conversation.ID] = conversation
		if _, err := NewService(store, store, nil).Mark(context.Background(), scopeOf(me, enums.OperatorRoleOperator), conversation.ID, tc.amount, tc.currency); !errors.Is(err, tc.wantErr) {
			t.Fatalf("Mark %q %q: got %v, want %v", tc.amount, tc.currency, err, tc.wantErr)
		}
	}
}

// TestOnlyTheVendorCreditedAndTheRolesAboveChangeASale: a correction or a removal by another operator is refused (ADR-0059).
func TestOnlyTheVendorCreditedAndTheRolesAboveChangeASale(t *testing.T) {
	vendor, other := id.New(), id.New()
	amount, currency := "200", "USD"

	for _, tc := range []struct {
		name    string
		caller  uuid.UUID
		role    enums.OperatorRole
		wantErr error
	}{
		{"the vendor credited", vendor, enums.OperatorRoleOperator, nil},
		{"a supervisor", other, enums.OperatorRoleSupervisor, nil},
		{"an admin", other, enums.OperatorRoleAdmin, nil},
		{"another operator", other, enums.OperatorRoleOperator, ErrNotTheVendor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newStore()
			sale := Sale{ID: id.New(), ConversationID: id.New(), OperatorID: vendor, Amount: "100", Currency: "ARS"}
			store.rows[sale.ID] = sale
			service := NewService(store, store, nil)

			updated, err := service.Update(context.Background(), scopeOf(tc.caller, tc.role), sale.ID, &amount, &currency)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Update: got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && (updated.Amount != "200" || updated.Currency != "USD" || updated.OperatorID != vendor) {
				t.Fatalf("the corrected sale %+v, want 200 USD still the vendor's", updated)
			}
			if err := service.Delete(context.Background(), scopeOf(tc.caller, tc.role), sale.ID); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Delete: got %v, want %v", err, tc.wantErr)
			}
		})
	}

	store := newStore()
	sale := Sale{ID: id.New(), OperatorID: vendor, Amount: "100", Currency: "ARS"}
	store.rows[sale.ID] = sale
	bad := "0"
	if _, err := NewService(store, store, nil).Update(context.Background(), scopeOf(vendor, enums.OperatorRoleOperator), sale.ID, &bad, nil); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("a correction to zero: got %v, want ErrInvalidAmount", err)
	}
}

// TestTheSalesOfAConversationHaveTheReachOfTheThread: the operator reads its own and the pool's, never another vendor's.
func TestTheSalesOfAConversationHaveTheReachOfTheThread(t *testing.T) {
	me, other := id.New(), id.New()
	store := newStore()
	mine, theirs, pooled := Conversation{ID: id.New(), OperatorID: &me}, Conversation{ID: id.New(), OperatorID: &other}, Conversation{ID: id.New()}
	for _, c := range []Conversation{mine, theirs, pooled} {
		store.conversations[c.ID] = c
	}
	service := NewService(store, store, nil)
	scope := scopeOf(me, enums.OperatorRoleOperator)

	for _, c := range []Conversation{mine, pooled} {
		if _, err := service.OfConversation(context.Background(), scope, c.ID); err != nil {
			t.Fatalf("OfConversation %v: %v", c.ID, err)
		}
	}
	if _, err := service.OfConversation(context.Background(), scope, theirs.ID); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("another vendor's: got %v, want ErrConversationNotFound", err)
	}
}
