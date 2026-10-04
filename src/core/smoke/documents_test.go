package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// The bucket docker-compose.smoke.yml names in FIREBASE_STORAGE_BUCKET: the case creates it, because the emulator starts empty.
const smokeBucket = "sonia-smoke"

// TestAnUploadedDocumentIsKeptSealedAndWaitsForTheWorker is F06's upload through the real process and a fake-gcs-server: the row
// in pending with the notice it accepted, and in the store only ciphertext that the document key opens (ADR-0064, ADR-0066).
func TestAnUploadedDocumentIsKeptSealedAndWaitsForTheWorker(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	store := os.Getenv("SONIA_SMOKE_STORAGE_URL")
	if store == "" {
		t.Fatalf("SONIA_SMOKE_STORAGE_URL is not set: the smoke reads the store to prove the file left sealed")
	}
	sealer := documentCipher(t)
	createBucket(t, store)

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}
	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}
	adminID, agentID := id.New(), id.New()
	email := "documents-" + adminID.String() + "@coop.test"

	// Children first: the documents point at the base, the base at the agent, the documents at the admin.
	t.Cleanup(func() {
		for _, step := range []struct {
			what, sql string
			arg       uuid.UUID
		}{
			{"the documents", `DELETE FROM documents WHERE uploaded_by_operator_id = $1`, adminID},
			{"the base", `DELETE FROM knowledge_bases WHERE ai_agent_id = $1`, agentID},
			{"the agent", `DELETE FROM ai_agents WHERE id = $1`, agentID},
			{"the admin", `DELETE FROM operators WHERE id = $1`, adminID},
		} {
			if _, err := pool.Exec(context.Background(), step.sql, step.arg); err != nil {
				t.Errorf("cleaning up %s: %v", step.what, err)
			}
		}
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
		VALUES ($1, $2, 'Recorrido', 'Documentos', $3, $4, 'admin', true)`, adminID, account, email, hash); err != nil {
		t.Fatalf("seeding the admin: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ai_agents (id, account_id, name, default_mode, llm_provider, llm_model)
		VALUES ($1, $2, $3, 'autonomous', 'cohere', 'command-a-03-2025')`, agentID, account, "Documentos "+agentID.String()); err != nil {
		t.Fatalf("seeding the agent: %v", err)
	}
	token := login(t, core, email, walkerPassword)
	base := "/api/v1/ai-agents/" + agentID.String() + "/documents"

	status, body := call(t, core, http.MethodGet, "/api/v1/documents/notice", token, "")
	if status != http.StatusOK {
		t.Fatalf("GET notice: got %d - %s", status, body)
	}
	var notice struct{ Version int }
	if err := json.Unmarshal([]byte(body), &notice); err != nil || notice.Version < 1 {
		t.Fatalf("the notice carries no version: %s", body)
	}

	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\nAlquiler: $320.000 + expensas " + agentID.String() + "\n%%EOF\n")
	status, body = upload(t, core, base, token, "precios.pdf", pdf, notice.Version)
	if status != http.StatusCreated {
		t.Fatalf("POST: got %d, want 201 - %s", status, body)
	}
	documentID := stringField(t, body, "id")

	var objectKey, rowStatus string
	var terms int16
	var size, total int64
	if err := pool.QueryRow(ctx, `
		SELECT d.object_key, d.status, d.personal_data_terms_version, d.size_bytes, kb.size_bytes_total
		FROM   documents d JOIN knowledge_bases kb ON kb.id = d.knowledge_base_id
		WHERE  d.id = $1`, documentID).Scan(&objectKey, &rowStatus, &terms, &size, &total); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if rowStatus != "pending" || int(terms) != notice.Version || size != int64(len(pdf)) || total != size {
		t.Fatalf("the row is %s with notice %d and %d bytes of %d, want pending, the notice and the file's size", rowStatus, terms, size, total)
	}

	sealed := download(t, store, objectKey)
	if bytes.Contains(sealed, []byte("%PDF-")) || bytes.Contains(sealed, []byte("320.000")) {
		t.Fatalf("the store holds the document in the clear")
	}
	opened, err := sealer.DecryptBytes(sealed)
	if err != nil || !bytes.Equal(opened, pdf) {
		t.Fatalf("the document key does not open the object back into the PDF: %v", err)
	}

	t.Run("el mismo archivo otra vez se rechaza", func(t *testing.T) {
		status, body := upload(t, core, base, token, "copia.pdf", pdf, notice.Version)
		if status != http.StatusConflict {
			t.Fatalf("got %d, want 409 - %s", status, body)
		}
	})

	t.Run("la baja marca la fila, devuelve el tamaño y deja el objeto", func(t *testing.T) {
		if status, body := call(t, core, http.MethodDelete, base+"/"+documentID, token, ""); status != http.StatusNoContent {
			t.Fatalf("DELETE: got %d - %s", status, body)
		}
		var deleted bool
		if err := pool.QueryRow(ctx, `
			SELECT d.deleted_at IS NOT NULL, kb.size_bytes_total
			FROM   documents d JOIN knowledge_bases kb ON kb.id = d.knowledge_base_id
			WHERE  d.id = $1`, documentID).Scan(&deleted, &total); err != nil || !deleted || total != 0 {
			t.Fatalf("after the removal: deleted %v, total %d, %v - want marked and zero", deleted, total, err)
		}
		if len(download(t, store, objectKey)) == 0 {
			t.Fatalf("the object went with the row, and ADR-0064 regla 9 keeps it")
		}
	})
}

func documentCipher(t *testing.T) *encryption.Cipher {
	t.Helper()
	key, err := encryption.DecodeKey(os.Getenv("SONIA_SMOKE_DOCUMENT_ENCRYPTION_KEY"))
	if err != nil {
		t.Fatalf("SONIA_SMOKE_DOCUMENT_ENCRYPTION_KEY: %v", err)
	}
	cipher, err := encryption.New(key)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	return cipher
}

// createBucket takes a 409 as done: the emulator lives as long as the stack, and a second run finds the bucket there.
func createBucket(t *testing.T, store string) {
	t.Helper()
	res, err := http.Post(store+"/storage/v1/b?project=sonia", "application/json", strings.NewReader(`{"name":"`+smokeBucket+`"}`))
	if err != nil {
		t.Fatalf("creating the bucket: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusConflict {
		payload, _ := io.ReadAll(res.Body)
		t.Fatalf("creating the bucket: got %d - %s", res.StatusCode, payload)
	}
}

func download(t *testing.T, store, key string) []byte {
	t.Helper()
	res, err := http.Get(store + "/storage/v1/b/" + smokeBucket + "/o/" + url.PathEscape(key) + "?alt=media")
	if err != nil {
		t.Fatalf("reading the object: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reading the object %s: got %d - %s", key, res.StatusCode, payload)
	}
	return payload
}

// upload sends what the admin panel sends: the file with its type, and the version of the notice it showed.
func upload(t *testing.T, core, path, token, filename string, data []byte, terms int) (int, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", "application/pdf")
	part, err := w.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	_, _ = part.Write(data)
	_ = w.WriteField("personal_data_terms_version", strconv.Itoa(terms))
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, core+path, &body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = res.Body.Close() }()
	payload, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(payload)
}
