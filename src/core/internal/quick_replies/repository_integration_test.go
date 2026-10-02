package quickreplies

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var rollback = errors.New("quick_replies: rolling back the integration case")

func integrationPool(t *testing.T) db.Pool {
	t.Helper()
	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}
	pool, err := db.Open(context.Background(), db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestQuickRepliesAgainstRealDDL is ADR-0060 against real DDL: the title unique in any casing, the search, the pause and the delete.
func TestQuickRepliesAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		var account uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		admin := id.New()
		if _, err := exec.Exec(ctx, `INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Prueba', 'Respuestas', $3, 'not-a-real-hash', 'admin', true)`, admin, account, admin.String()+"@sonia.test"); err != nil {
			t.Fatalf("seed an admin: %v", err)
		}
		// The account is a single row (7.3), so the case marks its replies with a token instead of owning an account.
		token := id.New().String()
		repo := NewRepository(exec)

		hours := QuickReply{ID: id.New(), AccountID: account, Title: " Horarios " + token, Content: "De 9 a 18.\nSábados de 9 a 13. " + token, Active: true, CreatedByOperatorID: admin}
		if err := repo.Create(ctx, hours); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := repo.ByID(ctx, account, hours.ID)
		if err != nil || got.Title != "Horarios "+token || got.Content != hours.Content || !got.Active || got.CreatedByOperatorID != admin {
			t.Fatalf("ByID: got %+v and %v, want it trimmed with its line breaks", got, err)
		}
		if err := repo.Create(ctx, QuickReply{ID: id.New(), AccountID: account, Title: "HORARIOS " + token, Content: "x", Active: true, CreatedByOperatorID: admin}); !errors.Is(err, ErrDuplicateTitle) {
			t.Fatalf("the same title in another casing: got %v, want ErrDuplicateTitle", err)
		}

		promo := QuickReply{ID: id.New(), AccountID: account, Title: "Promo 50% " + token, Content: "Mitad de precio " + token, Active: true, CreatedByOperatorID: admin}
		if err := repo.Create(ctx, promo); err != nil {
			t.Fatalf("Create: %v", err)
		}
		paused := false
		if err := repo.Update(ctx, account, promo.ID, nil, nil, &paused); err != nil {
			t.Fatalf("pausing: %v", err)
		}
		if got, _ := repo.ByID(ctx, account, promo.ID); got.Active || got.Title != promo.Title || got.Content != promo.Content {
			t.Fatalf("after the pause: %+v, want only active changed", got)
		}

		for _, tc := range []struct {
			text       string
			onlyActive bool
			want       int
		}{
			{token, false, 2},
			{token, true, 1},
			{"sábados de 9 a 13. " + token, false, 1},
			{"50% " + token, false, 1},
			{"5_% " + token, false, 0},
		} {
			page, err := repo.List(ctx, account, tc.text, tc.onlyActive, 20, 0)
			if err != nil || page.Total != tc.want || len(page.Items) != tc.want {
				t.Fatalf("List %q only active %v: got %d of %d and %v, want %d", tc.text, tc.onlyActive, len(page.Items), page.Total, err, tc.want)
			}
		}
		if _, err := repo.ByID(ctx, id.New(), promo.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a reply read from another account: got %v, want ErrNotFound", err)
		}

		if err := repo.SoftDelete(ctx, account, hours.ID); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if err := repo.SoftDelete(ctx, account, hours.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleting twice: got %v, want ErrNotFound", err)
		}
		if err := repo.Update(ctx, account, hours.ID, nil, nil, &paused); !errors.Is(err, ErrNotFound) {
			t.Fatalf("updating a deleted reply: got %v, want ErrNotFound", err)
		}
		if err := repo.Create(ctx, QuickReply{ID: id.New(), AccountID: account, Title: "Horarios " + token, Content: "nuevo", Active: true, CreatedByOperatorID: admin}); err != nil {
			t.Fatalf("the title freed by the delete: %v", err)
		}
		var kept int
		if err := exec.QueryRow(ctx, `SELECT count(*) FROM quick_replies WHERE id = $1`, hours.ID).Scan(&kept); err != nil || kept != 1 {
			t.Fatalf("the deleted reply's row: %d and %v, want it kept (C09)", kept, err)
		}

		// Last, because an UPDATE has no ON CONFLICT and its violation aborts the transaction around it (SQLSTATE 25P02).
		taken := "horarios " + token
		if err := repo.Update(ctx, account, promo.ID, &taken, nil, nil); !errors.Is(err, ErrDuplicateTitle) {
			t.Fatalf("renaming onto a taken title: got %v, want ErrDuplicateTitle", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
