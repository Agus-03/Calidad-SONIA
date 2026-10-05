package documents

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var rollback = errors.New("documents: rolling back the integration case")

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

// TestTheKnowledgeBaseAgainstRealDDL is ADR-0064 and ADR-0066 against the real schema: one base per agent, the row in pending
// with the notice it accepted, the size it moves, the duplicate and the logical removal.
func TestTheKnowledgeBaseAgainstRealDDL(t *testing.T) {
	ctx := context.Background()
	err := db.InTx(ctx, integrationPool(t), func(exec db.Executor) error {
		var account uuid.UUID
		if err := exec.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
			t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
		}
		admin, agent := id.New(), id.New()
		if _, err := exec.Exec(ctx, `INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Prueba', 'Documentos', $3, 'not-a-real-hash', 'admin', true)`, admin, account, admin.String()+"@sonia.test"); err != nil {
			t.Fatalf("seed an admin: %v", err)
		}
		if _, err := exec.Exec(ctx, `INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
			VALUES ($1, $2, $3, 'autonomous', 'anthropic', 'claude-sonnet-5-5')`, agent, account, "Agente "+agent.String()); err != nil {
			t.Fatalf("seed an agent: %v", err)
		}
		repo := NewRepository(exec)

		if exists, err := repo.AgentExists(ctx, account, agent); err != nil || !exists {
			t.Fatalf("AgentExists: got %v and %v, want the agent just seeded", exists, err)
		}
		if exists, _ := repo.AgentExists(ctx, id.New(), agent); exists {
			t.Fatalf("AgentExists answered true for another account")
		}
		if _, found, err := repo.KnowledgeBase(ctx, agent); err != nil || found {
			t.Fatalf("KnowledgeBase before any upload: found %v and %v, want none", found, err)
		}
		if _, err := repo.KnowledgeBaseForUpdate(ctx, agent); !errors.Is(err, errNoKnowledgeBase) {
			t.Fatalf("KnowledgeBaseForUpdate before any upload: got %v, want errNoKnowledgeBase", err)
		}

		kb, err := repo.EnsureKnowledgeBase(ctx, KnowledgeBase{ID: id.New(), AIAgentID: agent, CollectionName: CollectionName(agent)})
		if err != nil || kb.CollectionName != "kb_"+agent.String() || kb.SizeBytesTotal != 0 {
			t.Fatalf("EnsureKnowledgeBase: got %+v and %v", kb, err)
		}
		// The second first upload: the partial unique index answers with the base that already exists, not a second one.
		again, err := repo.EnsureKnowledgeBase(ctx, KnowledgeBase{ID: id.New(), AIAgentID: agent, CollectionName: CollectionName(agent)})
		if err != nil || again.ID != kb.ID {
			t.Fatalf("EnsureKnowledgeBase twice: got %s and %v, want the same base %s", again.ID, err, kb.ID)
		}

		doc := Document{
			ID: id.New(), KnowledgeBaseID: kb.ID, Filename: "precios.pdf", MIMEType: pdfMIMEType, SizeBytes: 1234,
			Checksum: "abc123", Status: enums.DocumentStatusPending, UploadedByOperatorID: admin, PersonalDataTermsVersion: NoticeVersion,
		}
		doc.ObjectKey = ObjectKey(kb.ID, doc.ID)
		saved, err := repo.InsertDocument(ctx, doc)
		if err != nil || saved.CreatedAt.IsZero() {
			t.Fatalf("InsertDocument: got %+v and %v, want the row with its creation time", saved, err)
		}
		if err := repo.AddSize(ctx, kb.ID, doc.SizeBytes); err != nil {
			t.Fatalf("AddSize: %v", err)
		}

		locked, err := repo.KnowledgeBaseForUpdate(ctx, agent)
		if err != nil || locked.SizeBytesTotal != 1234 {
			t.Fatalf("KnowledgeBaseForUpdate: got %+v and %v, want the 1234 bytes", locked, err)
		}
		if taken, err := repo.ChecksumTaken(ctx, kb.ID, "abc123"); err != nil || !taken {
			t.Fatalf("ChecksumTaken: got %v and %v, want the checksum taken", taken, err)
		}

		listed, err := repo.List(ctx, account, agent)
		if err != nil || len(listed) != 1 {
			t.Fatalf("List: got %d and %v, want the one document", len(listed), err)
		}
		got := listed[0]
		if got.Status != enums.DocumentStatusPending || got.PersonalDataTermsVersion != NoticeVersion || got.ObjectKey != doc.ObjectKey || got.ErrorMessage != nil {
			t.Fatalf("List: got %+v, want it pending, with the notice, its key and no error", got)
		}
		if other, _ := repo.List(ctx, id.New(), agent); len(other) != 0 {
			t.Fatalf("List answered %d documents to another account", len(other))
		}

		if _, err := repo.DocumentForUpdate(ctx, id.New(), agent, doc.ID); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("DocumentForUpdate from another account: got %v, want ErrDocumentNotFound", err)
		}
		if found, err := repo.DocumentForUpdate(ctx, account, agent, doc.ID); err != nil || found.ID != doc.ID {
			t.Fatalf("DocumentForUpdate: got %+v and %v", found, err)
		}
		if err := repo.SoftDeleteDocument(ctx, doc.ID); err != nil {
			t.Fatalf("SoftDeleteDocument: %v", err)
		}
		if err := repo.SoftDeleteDocument(ctx, doc.ID); !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("SoftDeleteDocument twice: got %v, want ErrDocumentNotFound", err)
		}
		if taken, _ := repo.ChecksumTaken(ctx, kb.ID, "abc123"); taken {
			t.Fatalf("a removed document still holds its checksum, so the same file could never come back (ADR-0070)")
		}
		if listed, _ := repo.List(ctx, account, agent); len(listed) != 0 {
			t.Fatalf("List after the removal: %d documents, want none", len(listed))
		}
		var deletedAt *string
		if err := exec.QueryRow(ctx, `SELECT deleted_at::text FROM documents WHERE id = $1`, doc.ID).Scan(&deletedAt); err != nil || deletedAt == nil {
			t.Fatalf("the row is gone or not marked: %v, want it kept with deleted_at (C09)", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: %v", err)
	}
}
