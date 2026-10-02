package operators

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// stubRoster is the database and the audit at once, answering from memory, so the roster is tested against its own rules.
type stubRoster struct {
	rows    map[uuid.UUID]Operator
	held    int
	audits  []stubAudit
	listErr error
}

type stubAudit struct {
	actor, target uuid.UUID
	action        string
	changes       string
}

func newRoster(rows ...Operator) *stubRoster {
	s := &stubRoster{rows: map[uuid.UUID]Operator{}}
	for _, o := range rows {
		s.rows[o.ID] = o
	}
	return s
}

func (s *stubRoster) service() *Roster {
	return NewRoster(RosterOptions{
		Repo: s,
		Tx: func(ctx context.Context, fn func(RosterStore, Auditor) error) error {
			return fn(s, s)
		},
	})
}

func (s *stubRoster) List(_ context.Context, accountID uuid.UUID, f Filter, limit, offset int) (Page, error) {
	if s.listErr != nil {
		return Page{}, s.listErr
	}
	page := Page{Items: []Operator{}}
	for _, o := range s.rows {
		if o.AccountID == accountID && (f.Role == nil || o.Role == *f.Role) && (f.Active == nil || o.Active == *f.Active) {
			page.Items = append(page.Items, o)
		}
	}
	page.Total = len(page.Items)
	return page, nil
}

func (s *stubRoster) ByID(_ context.Context, operatorID uuid.UUID) (Operator, error) {
	o, ok := s.rows[operatorID]
	if !ok {
		return Operator{}, ErrNotFound
	}
	return o, nil
}

func (s *stubRoster) LockAdmins(_ context.Context, accountID uuid.UUID) (int, error) {
	n := 0
	for _, o := range s.rows {
		if o.AccountID == accountID && o.Role == enums.OperatorRoleAdmin && o.Active {
			n++
		}
	}
	return n, nil
}

func (s *stubRoster) ForUpdate(_ context.Context, accountID, operatorID uuid.UUID) (Operator, error) {
	o, ok := s.rows[operatorID]
	if !ok || o.AccountID != accountID {
		return Operator{}, ErrNotFound
	}
	return o, nil
}

func (s *stubRoster) taken(email string, except uuid.UUID) bool {
	for _, o := range s.rows {
		if o.ID != except && o.Email == normalizeEmail(email) {
			return true
		}
	}
	return false
}

func (s *stubRoster) Register(_ context.Context, o Operator) error {
	if s.taken(o.Email, o.ID) {
		return ErrDuplicateEmail
	}
	s.rows[o.ID] = o
	return nil
}

func (s *stubRoster) Update(_ context.Context, o Operator) error {
	if s.taken(o.Email, o.ID) {
		return ErrDuplicateEmail
	}
	o.PasswordHash = s.rows[o.ID].PasswordHash
	s.rows[o.ID] = o
	return nil
}

func (s *stubRoster) SetPassword(_ context.Context, _, operatorID uuid.UUID, hash, replaced string) error {
	o := s.rows[operatorID]
	if o.PasswordHash != replaced {
		return ErrPasswordChanged
	}
	o.PasswordHash = hash
	s.rows[operatorID] = o
	return nil
}

func (s *stubRoster) SoftDelete(_ context.Context, _, operatorID uuid.UUID) error {
	delete(s.rows, operatorID)
	return nil
}

func (s *stubRoster) HeldConversations(context.Context, uuid.UUID) (int, error) { return s.held, nil }

func (s *stubRoster) RecordOperatorChange(_ context.Context, actorID, operatorID uuid.UUID, action string, changes []byte) error {
	s.audits = append(s.audits, stubAudit{actor: actorID, target: operatorID, action: action, changes: string(changes)})
	return nil
}

var rosterAccount = id.New()

func person(role enums.OperatorRole) Operator {
	return Operator{
		ID: id.New(), AccountID: rosterAccount, Name: "Ana", Lastname: "Paz", Email: id.New().String() + "@sonia.test",
		PasswordHash: "not-a-real-hash", Role: role, Active: true,
	}
}

func as(o Operator) Scope { return Scope{AccountID: o.AccountID, OperatorID: o.ID} }

func ptr[T any](v T) *T { return &v }

// TestTheAdminRegistersAPersonAndTheAuditKeepsNoHash: the row and its audit, and the password nowhere in audit_logs (ADR-0062).
func TestTheAdminRegistersAPersonAndTheAuditKeepsNoHash(t *testing.T) {
	admin := person(enums.OperatorRoleAdmin)
	store := newRoster(admin)

	created, err := store.service().Create(context.Background(), as(admin), NewOperator{
		Name: " Bruno ", Lastname: "Sosa", Email: " Bruno@Sonia.Test ", Password: "una clave larga", Role: enums.OperatorRoleOperator,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "Bruno" || created.Email != "bruno@sonia.test" || !created.Active || Verify(created.PasswordHash, "una clave larga") != nil {
		t.Fatalf("created %+v, want it trimmed, lowercased, active and with the password hashed", created)
	}
	if len(store.audits) != 1 || store.audits[0].action != auditCreate || store.audits[0].actor != admin.ID {
		t.Fatalf("audits %+v, want one create by the admin", store.audits)
	}
	if strings.Contains(store.audits[0].changes, created.PasswordHash) || strings.Contains(store.audits[0].changes, "password") {
		t.Fatalf("the audit %s carries the password", store.audits[0].changes)
	}
	if _, err := store.service().Create(context.Background(), as(admin), NewOperator{
		Name: "Otro", Lastname: "Sosa", Email: "BRUNO@sonia.test", Password: "una clave larga", Role: enums.OperatorRoleOperator,
	}); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("the same address again: got %v, want ErrDuplicateEmail", err)
	}
}

// TestWhatTheAdminTypesIsRefusedBeforeAnythingIsWritten covers each field of the form, and no audit for any of them.
func TestWhatTheAdminTypesIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	admin := person(enums.OperatorRoleAdmin)
	good := NewOperator{Name: "Ana", Lastname: "Paz", Email: "ana@sonia.test", Password: "ocho car", Role: enums.OperatorRoleOperator}

	for _, tc := range []struct {
		name string
		edit func(*NewOperator)
		want error
	}{
		{"a blank name", func(n *NewOperator) { n.Name = "  " }, ErrEmptyName},
		{"a blank lastname", func(n *NewOperator) { n.Lastname = "" }, ErrEmptyName},
		{"an address with a display name", func(n *NewOperator) { n.Email = "Ana <ana@sonia.test>" }, ErrInvalidEmail},
		{"no address at all", func(n *NewOperator) { n.Email = "ana" }, ErrInvalidEmail},
		{"a role that does not exist", func(n *NewOperator) { n.Role = "superadmin" }, ErrInvalidRole},
		{"seven characters", func(n *NewOperator) { n.Password = "siete c" }, ErrPasswordTooWeak},
		{"more than bcrypt reads", func(n *NewOperator) { n.Password = strings.Repeat("x", 73) }, ErrPasswordTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newRoster(admin)
			in := good
			tc.edit(&in)
			if _, err := store.service().Create(context.Background(), as(admin), in); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(store.rows) != 1 || len(store.audits) != 0 {
				t.Fatalf("%d rows and %d audits, want nothing written", len(store.rows), len(store.audits))
			}
		})
	}
}

// TestTheAdminCannotLockThemselvesOut: their own role, active, removal and a reset without the current password (ADR-0062).
func TestTheAdminCannotLockThemselvesOut(t *testing.T) {
	admin, other := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleAdmin)
	store := newRoster(admin, other)
	roster := store.service()
	ctx := context.Background()

	for name, patch := range map[string]Patch{
		"demote":     {Role: ptr(enums.OperatorRoleSupervisor)},
		"deactivate": {Active: ptr(false)},
	} {
		if _, err := roster.Update(ctx, as(admin), admin.ID, patch); !errors.Is(err, ErrSelf) {
			t.Fatalf("%s themselves: got %v, want ErrSelf", name, err)
		}
	}
	if _, err := roster.Delete(ctx, as(admin), admin.ID); !errors.Is(err, ErrSelf) {
		t.Fatalf("delete themselves: got %v, want ErrSelf", err)
	}
	if err := roster.ResetPassword(ctx, as(admin), admin.ID, "una clave larga"); !errors.Is(err, ErrSelf) {
		t.Fatalf("reset their own: got %v, want ErrSelf", err)
	}
	// The whole row sent back with the same role is a rename, and that is theirs to do.
	renamed, err := roster.Update(ctx, as(admin), admin.ID, Patch{Name: ptr("Ana María"), Role: ptr(enums.OperatorRoleAdmin), Active: ptr(true)})
	if err != nil || renamed.Operator.Name != "Ana María" {
		t.Fatalf("a rename of themselves: got %+v and %v", renamed, err)
	}
	if len(store.audits) != 1 {
		t.Fatalf("%d audits, want only the rename", len(store.audits))
	}
}

// TestTheAccountKeepsAnActiveAdmin: the lock counts the admins, and the last one is neither demoted, deactivated nor deleted.
func TestTheAccountKeepsAnActiveAdmin(t *testing.T) {
	ctx := context.Background()
	caller, last := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleAdmin)
	// The caller lost the role between its request and this check, which is the race the lock exists for.
	caller.Role = enums.OperatorRoleSupervisor
	store := newRoster(caller, last)
	roster := store.service()

	if _, err := roster.Update(ctx, as(caller), last.ID, Patch{Role: ptr(enums.OperatorRoleOperator)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote the last: got %v, want ErrLastAdmin", err)
	}
	if _, err := roster.Update(ctx, as(caller), last.ID, Patch{Active: ptr(false)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deactivate the last: got %v, want ErrLastAdmin", err)
	}
	if _, err := roster.Delete(ctx, as(caller), last.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete the last: got %v, want ErrLastAdmin", err)
	}

	// With a second admin the same change goes through.
	second := person(enums.OperatorRoleAdmin)
	store.rows[second.ID] = second
	if _, err := roster.Update(ctx, as(second), last.ID, Patch{Role: ptr(enums.OperatorRoleSupervisor)}); err != nil {
		t.Fatalf("demote one of two: %v", err)
	}
}

// TestDeactivatingAVendorTellsWhatTheyStillHold is the decision of the group: allowed, and the admin is told (ADR-0062).
func TestDeactivatingAVendorTellsWhatTheyStillHold(t *testing.T) {
	admin, vendor := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleOperator)
	store := newRoster(admin, vendor)
	store.held = 3
	roster := store.service()

	held, err := roster.Update(context.Background(), as(admin), vendor.ID, Patch{Active: ptr(false)})
	if err != nil || held.Operator.Active || held.Conversations != 3 {
		t.Fatalf("got %+v and %v, want inactive and three held", held, err)
	}
	var ch change
	if err := json.Unmarshal([]byte(store.audits[0].changes), &ch); err != nil || ch.Before == nil || !ch.Before.Active || ch.After.Active {
		t.Fatalf("the audit %s, want before active and after not", store.audits[0].changes)
	}

	// Saving the same thing again writes nothing, audit included (ADR-0038, regla 9).
	if _, err := roster.Update(context.Background(), as(admin), vendor.ID, Patch{Active: ptr(false)}); err != nil || len(store.audits) != 1 {
		t.Fatalf("a save with no change: %v and %d audits, want one", err, len(store.audits))
	}

	left, err := roster.Delete(context.Background(), as(admin), vendor.ID)
	if err != nil || left != 3 || store.audits[1].action != auditDelete {
		t.Fatalf("Delete: %d held, %v, audits %+v", left, err, store.audits)
	}
	if _, err := roster.Delete(context.Background(), as(admin), vendor.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: got %v, want ErrNotFound", err)
	}
}

// TestAForgottenPasswordIsResetByTheAdmin is the way back for whoever forgot it; the audit says it happened and not what it became.
func TestAForgottenPasswordIsResetByTheAdmin(t *testing.T) {
	admin, vendor := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleOperator)
	store := newRoster(admin, vendor)
	roster := store.service()

	if err := roster.ResetPassword(context.Background(), as(admin), vendor.ID, "corta"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("a short one: got %v, want ErrPasswordTooWeak", err)
	}
	if err := roster.ResetPassword(context.Background(), as(admin), vendor.ID, "la nueva del vendedor"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if Verify(store.rows[vendor.ID].PasswordHash, "la nueva del vendedor") != nil {
		t.Fatal("the vendor's password was not replaced")
	}
	if len(store.audits) != 1 || store.audits[0].changes != `{"password":"reset"}` || store.audits[0].target != vendor.ID {
		t.Fatalf("audits %+v, want one password reset of the vendor", store.audits)
	}
	if err := roster.ResetPassword(context.Background(), as(admin), id.New(), "la nueva del vendedor"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a stranger: got %v, want ErrNotFound", err)
	}
}

// TestEveryoneChangesTheirOwnPasswordWithTheCurrentOne, so a stolen session alone does not take the account (ADR-0062).
func TestEveryoneChangesTheirOwnPasswordWithTheCurrentOne(t *testing.T) {
	vendor := person(enums.OperatorRoleOperator)
	hash, err := Hash("la de siempre")
	if err != nil {
		t.Fatal(err)
	}
	vendor.PasswordHash = hash
	store := newRoster(vendor)
	roster := store.service()
	ctx := context.Background()

	if err := roster.ChangePassword(ctx, as(vendor), "otra cualquiera", "la nueva y larga"); !errors.Is(err, ErrWrongCurrentPassword) {
		t.Fatalf("a wrong current one: got %v, want ErrWrongCurrentPassword", err)
	}
	if err := roster.ChangePassword(ctx, as(vendor), "la de siempre", "corta"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("a short new one: got %v, want ErrPasswordTooWeak", err)
	}
	if err := roster.ChangePassword(ctx, as(vendor), "la de siempre", "la nueva y larga"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if Verify(store.rows[vendor.ID].PasswordHash, "la nueva y larga") != nil || len(store.audits) != 1 || store.audits[0].actor != vendor.ID {
		t.Fatalf("the change was not written or not audited as theirs: %+v", store.audits)
	}
	if err := roster.ChangePassword(ctx, Scope{AccountID: rosterAccount, OperatorID: id.New()}, "x", "la nueva y larga"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a caller that is gone: got %v, want ErrNotFound", err)
	}
}

// TestAPatchThatBreaksTheRowIsRefused: the merged row is validated, and a taken address is the store's answer.
func TestAPatchThatBreaksTheRowIsRefused(t *testing.T) {
	admin, vendor, other := person(enums.OperatorRoleAdmin), person(enums.OperatorRoleOperator), person(enums.OperatorRoleOperator)
	store := newRoster(admin, vendor, other)
	roster := store.service()

	for _, tc := range []struct {
		patch Patch
		want  error
	}{
		{Patch{Name: ptr(" ")}, ErrEmptyName},
		{Patch{Email: ptr("no-es-un-correo")}, ErrInvalidEmail},
		{Patch{Role: ptr(enums.OperatorRole("owner"))}, ErrInvalidRole},
		{Patch{Email: ptr(strings.ToUpper(other.Email))}, ErrDuplicateEmail},
	} {
		if _, err := roster.Update(context.Background(), as(admin), vendor.ID, tc.patch); !errors.Is(err, tc.want) {
			t.Fatalf("patch %+v: got %v, want %v", tc.patch, err, tc.want)
		}
	}
	if _, err := roster.Update(context.Background(), as(admin), id.New(), Patch{Name: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a stranger: got %v, want ErrNotFound", err)
	}
	if len(store.audits) != 0 {
		t.Fatalf("%d audits, want none", len(store.audits))
	}
}
