package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// mount builds the real router, because the paths and the roles only exist in routes.go.
func mount(role enums.OperatorRole, f fixture) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(f.service, nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return f.scope.OperatorID, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: f.scope.OperatorID, AccountID: f.scope.AccountID, Role: role}, nil
		},
	})
}

func call(engine http.Handler, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// form builds the body a browser sends: the file with its own type, and the version of the notice the admin accepted.
func form(t *testing.T, filename, fileType string, data []byte, terms string) (string, []byte) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if data != nil {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
		header.Set("Content-Type", fileType)
		part, err := w.CreatePart(header)
		if err != nil {
			t.Fatalf("CreatePart: %v", err)
		}
		_, _ = part.Write(data)
	}
	if terms != "" {
		_ = w.WriteField("personal_data_terms_version", terms)
	}
	_ = w.Close()
	return w.FormDataContentType(), body.Bytes()
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}

// TestOnlyTheAdminTouchesTheKnowledgeBase: the base is part of the agent's configuration, which Anexo B gives to the Admin.
func TestOnlyTheAdminTouchesTheKnowledgeBase(t *testing.T) {
	f := newFixture()
	base := "/ai-agents/" + f.agentID.String() + "/documents"
	contentType, body := form(t, "a.pdf", "application/pdf", pdf("x"), "1")
	for _, role := range []enums.OperatorRole{enums.OperatorRoleOperator, enums.OperatorRoleSupervisor} {
		engine := mount(role, f)
		for _, w := range []struct {
			method, path, contentType string
			body                      []byte
		}{
			{http.MethodGet, "/documents/notice", "", nil},
			{http.MethodGet, base, "", nil},
			{http.MethodPost, base, contentType, body},
			{http.MethodDelete, base + "/" + id.New().String(), "", nil},
		} {
			if rec := call(engine, w.method, w.path, w.contentType, w.body); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s by %s: got %d, want 403", w.method, w.path, role, rec.Code)
			}
		}
	}
}

// TestTheDocumentRoutesAnswerWithTheirCodes walks the four routes as the admin, because a wrong path or method compiles.
func TestTheDocumentRoutesAnswerWithTheirCodes(t *testing.T) {
	f := newFixture()
	engine := mount(enums.OperatorRoleAdmin, f)
	base := "/ai-agents/" + f.agentID.String() + "/documents"

	rec := call(engine, http.MethodGet, "/documents/notice", "", nil)
	var notice noticeResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &notice) != nil || notice.Version != NoticeVersion || notice.Text != NoticeText {
		t.Fatalf("GET notice: got %d - %s, want the text and the version in force", rec.Code, rec.Body)
	}

	contentType, body := form(t, "precios.pdf", "application/pdf", pdf("lista"), strconv.Itoa(NoticeVersion))
	rec = call(engine, http.MethodPost, base, contentType, body)
	var created documentResponse
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &created) != nil {
		t.Fatalf("POST: got %d - %s, want 201", rec.Code, rec.Body)
	}
	if created.Filename != "precios.pdf" || created.Status != "pending" || created.PersonalDataTermsVersion != NoticeVersion || created.CreatedAt == "" {
		t.Fatalf("POST answered %+v, want the file pending with the notice it accepted", created)
	}
	if strings.Contains(rec.Body.String(), "object_key") || strings.Contains(rec.Body.String(), "checksum") {
		t.Fatalf("POST exposes the object key or the checksum: %s", rec.Body)
	}

	rec = call(engine, http.MethodGet, base, "", nil)
	var listing listResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &listing) != nil {
		t.Fatalf("GET: got %d - %s", rec.Code, rec.Body)
	}
	if len(listing.Items) != 1 || listing.SizeBytesTotal != created.SizeBytes || listing.SizeBytesLimit != MaxKnowledgeBaseBytes {
		t.Fatalf("GET answered %+v, want the document, its size and the 100 MB", listing)
	}

	if rec := call(engine, http.MethodDelete, base+"/"+created.ID, "", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: got %d - %s, want 204", rec.Code, rec.Body)
	}
	if rec := call(engine, http.MethodDelete, base+"/"+created.ID, "", nil); rec.Code != http.StatusNotFound || errorCode(t, rec) != "document_not_found" {
		t.Fatalf("DELETE twice: got %d - %s, want 404 document_not_found", rec.Code, rec.Body)
	}
}

// TestEveryRefusalHasItsCode: the frontend switches on the code, so each refusal of the service needs one of its own.
func TestEveryRefusalHasItsCode(t *testing.T) {
	f := newFixture()
	engine := mount(enums.OperatorRoleAdmin, f)
	base := "/ai-agents/" + f.agentID.String() + "/documents"

	ok := pdf("contrato")
	ct, body := form(t, "contrato.pdf", "application/pdf", ok, "1")
	if rec := call(engine, http.MethodPost, base, ct, body); rec.Code != http.StatusCreated {
		t.Fatalf("the first upload: %d - %s", rec.Code, rec.Body)
	}

	multipartOf := func(filename, fileType string, data []byte, terms string) func() (string, []byte) {
		return func() (string, []byte) { return form(t, filename, fileType, data, terms) }
	}
	raw := func(contentType string, body []byte) func() (string, []byte) {
		return func() (string, []byte) { return contentType, body }
	}
	for _, tc := range []struct {
		name   string
		path   string
		build  func() (string, []byte)
		status int
		code   string
	}{
		{"el mismo archivo", base, raw(ct, body), http.StatusConflict, "document_duplicate"},
		{"sin aceptar el aviso", base, multipartOf("b.pdf", "application/pdf", pdf("b"), ""), http.StatusBadRequest, "notice_not_accepted"},
		{"una imagen", base, multipartOf("b.png", "image/png", []byte("png"), "1"), http.StatusBadRequest, "not_a_pdf"},
		{"un nombre de mas de 255 caracteres", base, multipartOf(strings.Repeat("a", 300)+".pdf", "application/pdf", pdf("largo"), "1"), http.StatusBadRequest, "invalid_filename"},
		{"un archivo vacio", base, multipartOf("b.pdf", "application/pdf", []byte{}, "1"), http.StatusBadRequest, "empty_file"},
		{"sin archivo", base, multipartOf("", "", nil, "1"), http.StatusBadRequest, "missing_file"},
		{"un cuerpo que no es multipart", base, raw("application/json", []byte(`{}`)), http.StatusBadRequest, "missing_file"},
		{"un agente que no existe", "/ai-agents/" + id.New().String() + "/documents", raw(ct, body), http.StatusNotFound, "ai_agent_not_found"},
		{"un id de agente roto", "/ai-agents/no-es-uuid/documents", raw(ct, body), http.StatusBadRequest, "invalid_ai_agent_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contentType, payload := tc.build()
			rec := call(engine, http.MethodPost, tc.path, contentType, payload)
			if rec.Code != tc.status || errorCode(t, rec) != tc.code {
				t.Fatalf("got %d - %s, want %d %s", rec.Code, rec.Body, tc.status, tc.code)
			}
		})
	}

	if rec := call(engine, http.MethodGet, "/ai-agents/"+id.New().String()+"/documents", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET of another agent: got %d, want 404", rec.Code)
	}
	if rec := call(engine, http.MethodGet, "/ai-agents/no-es-uuid/documents", "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET with a broken id: got %d, want 400", rec.Code)
	}
	if rec := call(engine, http.MethodDelete, base+"/no-es-uuid", "", nil); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "invalid_document_id" {
		t.Fatalf("DELETE with a broken id: got %d - %s, want 400 invalid_document_id", rec.Code, rec.Body)
	}
	if rec := call(engine, http.MethodDelete, "/ai-agents/no-es-uuid/documents/"+id.New().String(), "", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE with a broken agent id: got %d, want 400", rec.Code)
	}

	// The store failing is the store's and not a bug: a 503 the admin can retry.
	f.objects.putErr = errBoom
	ct, body = form(t, "nuevo.pdf", "application/pdf", pdf("nuevo"), "1")
	if rec := call(engine, http.MethodPost, base, ct, body); rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "storage_unavailable" {
		t.Fatalf("a failing store: got %d - %s, want 503 storage_unavailable", rec.Code, rec.Body)
	}
	f.objects.putErr = nil

	// Anything the package does not know falls through as a 500.
	f.store.readErr = errBoom
	if rec := call(engine, http.MethodGet, base, "", nil); rec.Code != http.StatusInternalServerError {
		t.Fatalf("a failing read: got %d, want 500", rec.Code)
	}
}

// TestABodyPastTheBaseIsRefusedBeforeItIsRead: the request is bounded, so 101 MB never sit in the core's memory.
func TestABodyPastTheBaseIsRefusedBeforeItIsRead(t *testing.T) {
	f := newFixture()
	engine := mount(enums.OperatorRoleAdmin, f)
	ct, body := form(t, "enorme.pdf", "application/pdf", append(pdf(""), make([]byte, MaxKnowledgeBaseBytes+multipartOverhead)...), "1")
	rec := call(engine, http.MethodPost, "/ai-agents/"+f.agentID.String()+"/documents", ct, body)
	if rec.Code != http.StatusRequestEntityTooLarge || errorCode(t, rec) != "knowledge_base_full" {
		t.Fatalf("got %d - %s, want 413 knowledge_base_full", rec.Code, rec.Body)
	}
}
