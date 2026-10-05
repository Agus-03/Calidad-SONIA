package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
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
	qdrant := os.Getenv("SONIA_SMOKE_QDRANT_URL")
	if qdrant == "" {
		t.Fatalf("SONIA_SMOKE_QDRANT_URL is not set: the smoke counts the points the worker wrote")
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
	collection := "kb_" + agentID.String()

	// The collection the worker created for the agent: the sweep empties it but never drops it, and the smoke's Qdrant is the
	// development one (ADR-0072), so a run that left it would leave an empty collection behind for every agent it created.
	t.Cleanup(func() { dropCollection(t, qdrant, collection) })

	// Children first: the documents point at the base, the base at the agent, the documents at the admin.
	t.Cleanup(func() {
		for _, step := range []struct {
			what, sql string
			arg       uuid.UUID
		}{
			// The chunks the worker wrote first: they point at the document, which is why a removal keeps them (ADR-0064, regla 9).
			{"the chunks", `DELETE FROM document_chunks WHERE document_id IN (SELECT id FROM documents WHERE uploaded_by_operator_id = $1)`, adminID},
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

	// A PDF the worker can read, so the case follows it past pending: the core's half and the worker's half of F06 (ADR-0064).
	pdf := textPDF("Alquiler: $320.000 + expensas. Agente " + agentID.String())
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
	// Pending when the core wrote it; the worker may already have taken it by the time this reads.
	if rowStatus == "failed" || int(terms) != notice.Version || size != int64(len(pdf)) || total != size {
		t.Fatalf("the row is %s with notice %d and %d bytes of %d, want it on its way, the notice and the file's size", rowStatus, terms, size, total)
	}

	sealed := download(t, store, objectKey)
	if bytes.Contains(sealed, []byte("%PDF-")) || bytes.Contains(sealed, []byte("320.000")) {
		t.Fatalf("the store holds the document in the clear")
	}
	opened, err := sealer.DecryptBytes(sealed)
	if err != nil || !bytes.Equal(opened, pdf) {
		t.Fatalf("the document key does not open the object back into the PDF: %v", err)
	}

	t.Run("el worker lo fragmenta, lo vectoriza y lo deja indexed", func(t *testing.T) {
		awaitStatus(ctx, t, pool, documentID, "indexed")
		var chunks, versioned int
		var content string
		// chunking_version is the version of the brain's rules/chunking.yaml, which ADR-0076 took to 2.
		if err := pool.QueryRow(ctx, `
			SELECT count(*),
			       count(*) FILTER (WHERE chunking_version = 2 AND description_prompt_version IS NULL AND embedding_model = 'double'
			                          AND vector_id = id::text AND token_count > 0),
			       string_agg(content, ' ')
			FROM   document_chunks WHERE document_id = $1 AND deleted_at IS NULL`, documentID).Scan(&chunks, &versioned, &content); err != nil {
			t.Fatalf("reading the chunks: %v", err)
		}
		if chunks == 0 || versioned != chunks || !strings.Contains(content, "320.000") {
			t.Fatalf("%d chunks, %d with what made them, content %q - want the text of the PDF with its versions (ADR-0065, regla 9)", chunks, versioned, content)
		}
		if points := countPoints(t, qdrant, collection, documentID); points != chunks {
			t.Fatalf("Qdrant holds %d points for the document, want one per chunk: %d", points, chunks)
		}
	})

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
		// The worker's sweep takes the vectors out of Qdrant and retires the chunks, which stay for retrieval_logs (ADR-0064, regla 9).
		deadline := time.Now().Add(30 * time.Second)
		for {
			var live int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM document_chunks WHERE document_id = $1 AND deleted_at IS NULL`, documentID).Scan(&live); err != nil {
				t.Fatalf("reading the chunks: %v", err)
			}
			if live == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%d chunks are still live 30 s after the removal: the worker did not sweep it", live)
			}
			time.Sleep(500 * time.Millisecond)
		}
		if points := countPoints(t, qdrant, collection, documentID); points != 0 {
			t.Fatalf("Qdrant still holds %d points of a removed document", points)
		}
	})
}

// awaitStatus polls the row, because the worker takes the document on its own clock (ADR-0064, regla 7).
func awaitStatus(ctx context.Context, t *testing.T, pool db.Pool, documentID, want string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var status string
		var reason *string
		if err := pool.QueryRow(ctx, `SELECT status, error_message FROM documents WHERE id = $1`, documentID).Scan(&status, &reason); err != nil {
			t.Fatalf("reading the status: %v", err)
		}
		if status == want {
			return
		}
		if status == "failed" && reason != nil {
			t.Fatalf("the worker failed the document: %s", *reason)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the document is still %s after 60 s, want %s", status, want)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// countPoints asks Qdrant how many points carry the document, which is all the payload holds (ADR-0064, regla 3).
func countPoints(t *testing.T, qdrant, collection, documentID string) int {
	t.Helper()
	body := `{"exact": true, "filter": {"must": [{"key": "document_id", "match": {"value": "` + documentID + `"}}]}}`
	res, err := http.Post(qdrant+"/collections/"+collection+"/points/count", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("counting the points: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	var answer struct {
		Result struct{ Count int } `json:"result"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&answer) != nil {
		t.Fatalf("counting the points: got %d", res.StatusCode)
	}
	return answer.Result.Count
}

// dropCollection removes the agent's collection from Qdrant. A collection that is not there is fine: the worker creates it with
// the first document it indexes, and a case that failed before that never had one.
func dropCollection(t *testing.T, qdrant, collection string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, qdrant+"/collections/"+collection, nil)
	if err != nil {
		t.Errorf("dropping the collection: %v", err)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("dropping the collection: %v", err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		t.Errorf("dropping the collection: got %d", res.StatusCode)
	}
}

// textPDF builds the smallest PDF whose text an extractor reads as it would from a real one: one page, one line, Helvetica.
func textPDF(text string) []byte {
	stream := "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		"<< /Length " + strconv.Itoa(len(stream)) + " >>\nstream\n" + stream + "\nendstream",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = out.Len()
		out.WriteString(strconv.Itoa(i+1) + " 0 obj\n" + body + "\nendobj\n")
	}
	xref := out.Len()
	out.WriteString("xref\n0 " + strconv.Itoa(len(objects)+1) + "\n0000000000 65535 f \n")
	for _, offset := range offsets {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", offset))
	}
	out.WriteString("trailer\n<< /Size " + strconv.Itoa(len(objects)+1) + " /Root 1 0 R >>\nstartxref\n" + strconv.Itoa(xref) + "\n%%EOF\n")
	return out.Bytes()
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
