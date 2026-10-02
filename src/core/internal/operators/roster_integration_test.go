package operators

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/agents"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheRosterAgainstRealDDL is ADR-0062 on one transaction, whose refusals must leave it usable: writes, audit, held and freed address.
func TestTheRosterAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		roster := NewRoster(RosterOptions{
			Repo: repo,
			Tx: func(ctx context.Context, fn func(RosterStore, Auditor) error) error {
				return fn(repo, agents.NewRepository(exec))
			},
		})

		hash, err := Hash("la del admin")
		if err != nil {
			t.Fatal(err)
		}
		admin := Operator{ID: id.New(), AccountID: account, Name: "Admin", Lastname: "Prueba", Email: unique("admin"),
			PasswordHash: hash, Role: enums.OperatorRoleAdmin, Active: true}
		if err := repo.Register(ctx, admin); err != nil {
			t.Fatalf("Register the admin: %v", err)
		}
		scope := Scope{AccountID: account, OperatorID: admin.ID}

		vendor, err := roster.Create(ctx, scope, NewOperator{Name: "Vera", Lastname: "Venta", Email: unique("vendedora"),
			Password: "una clave larga", Role: enums.OperatorRoleOperator})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := roster.Create(ctx, scope, NewOperator{Name: "Otra", Lastname: "Vez", Email: strings.ToUpper(vendor.Email),
			Password: "una clave larga", Role: enums.OperatorRoleOperator}); !errors.Is(err, ErrDuplicateEmail) {
			t.Fatalf("the same address in capitals: got %v, want ErrDuplicateEmail", err)
		}
		var audited int
		var leaked bool
		if err := exec.QueryRow(ctx, `SELECT count(*), bool_or(strpos(changes_json::text, $3) > 0 OR strpos(changes_json::text, 'password') > 0) FROM audit_logs
			WHERE entity_type = 'operators' AND entity_id = $1 AND action = 'create' AND operator_id = $2`, vendor.ID, admin.ID, vendor.PasswordHash).Scan(&audited, &leaked); err != nil || audited != 1 || leaked {
			t.Fatalf("the create's audit: %d rows, a hash in it %v, %v; want one and no hash", audited, leaked, err)
		}

		var contact, inbox, agent uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT c.id, i.id, g.id FROM contacts c, inboxes i, ai_agents g LIMIT 1`).Scan(&contact, &inbox, &agent); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		for _, status := range []string{"active", "resolved"} {
			if _, err := exec.Exec(ctx, `INSERT INTO conversations (id, contact_id, inbox_id, ai_agent_id, operator_id, status, mode, last_inbound_at, window_expires_at)
				VALUES ($1, $2, $3, $4, $5, $6, 'off', now(), now() + interval '24 hours')`, id.New(), contact, inbox, agent, vendor.ID, status); err != nil {
				t.Fatalf("seed a %s conversation: %v", status, err)
			}
		}

		off := false
		held, err := roster.Update(ctx, scope, vendor.ID, Patch{Active: &off})
		if err != nil || held.Operator.Active || held.Conversations != 1 {
			t.Fatalf("deactivate: got %+v and %v, want inactive holding the one that is not resolved", held, err)
		}
		if _, err := roster.Update(ctx, scope, vendor.ID, Patch{Email: &admin.Email}); !errors.Is(err, ErrDuplicateEmail) {
			t.Fatalf("an address someone else has: got %v, want ErrDuplicateEmail", err)
		}

		operator := enums.OperatorRoleOperator
		page, err := roster.List(ctx, scope, Filter{Role: &operator, Active: &off}, 100, 0)
		if err != nil || !contains(page.Items, vendor.ID) || contains(page.Items, admin.ID) || page.Total < 1 {
			t.Fatalf("List of inactive operators: got %d of %d and %v, want the vendor and not the admin", len(page.Items), page.Total, err)
		}
		if locked, err := repo.LockAdmins(ctx, account); err != nil || locked < 1 {
			t.Fatalf("LockAdmins: %d and %v, want at least the admin", locked, err)
		}

		if err := roster.ResetPassword(ctx, scope, vendor.ID, "la nueva del vendedor"); err != nil {
			t.Fatalf("ResetPassword: %v", err)
		}
		if got, _ := repo.ByID(ctx, vendor.ID); Verify(got.PasswordHash, "la nueva del vendedor") != nil {
			t.Fatal("the reset did not replace the password")
		}
		if err := repo.SetPassword(ctx, account, vendor.ID, "otro", "un hash viejo"); !errors.Is(err, ErrPasswordChanged) {
			t.Fatalf("a stale hash in the predicate: got %v, want ErrPasswordChanged", err)
		}
		if err := roster.ChangePassword(ctx, scope, "la del admin", "la nueva del admin"); err != nil {
			t.Fatalf("ChangePassword: %v", err)
		}

		left, err := roster.Delete(ctx, scope, vendor.ID)
		if err != nil || left != 1 {
			t.Fatalf("Delete: %d and %v, want the one still held", left, err)
		}
		if _, err := repo.ByID(ctx, vendor.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("the deleted one: got %v, want ErrNotFound", err)
		}
		if _, err := roster.Create(ctx, scope, NewOperator{Name: "Vera", Lastname: "Nueva", Email: vendor.Email,
			Password: "una clave larga", Role: enums.OperatorRoleOperator}); err != nil {
			t.Fatalf("the address freed by the delete: %v", err)
		}

		var actions []string
		rows, err := exec.Query(ctx, `SELECT action FROM audit_logs WHERE entity_type = 'operators' AND entity_id = $1 ORDER BY created_at, action`, vendor.ID)
		if err != nil {
			t.Fatalf("read the audit: %v", err)
		}
		for rows.Next() {
			var action string
			if err := rows.Scan(&action); err != nil {
				t.Fatal(err)
			}
			actions = append(actions, action)
		}
		rows.Close()
		if len(actions) != 4 {
			t.Fatalf("the vendor's audit: %v, want create, the deactivation, the reset and the delete", actions)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

func contains(items []Operator, operatorID uuid.UUID) bool {
	for _, o := range items {
		if o.ID == operatorID {
			return true
		}
	}
	return false
}
