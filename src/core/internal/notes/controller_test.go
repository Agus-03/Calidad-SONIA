package notes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func mount(role enums.OperatorRole, d *desk) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(d.service, nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

func send(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
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

// TestTheNoteRoutesAnswerWithTheirCodes walks the four routes, because a wrong method or path compiles (ADR-0056).
func TestTheNoteRoutesAnswerWithTheirCodes(t *testing.T) {
	d := newDesk()
	conversationID := d.open(&testOperator)
	base := "/conversations/" + conversationID.String() + "/notes"
	engine := mount(enums.OperatorRoleOperator, d)

	if rec := send(engine, http.MethodPost, base, `{"content":"llamar el lunes"}`); rec.Code != http.StatusCreated ||
		!strings.Contains(rec.Body.String(), `"content":"llamar el lunes"`) {
		t.Fatalf("POST: got %d - body %s, want 201 with the note", rec.Code, rec.Body)
	}
	var noteID string
	for k := range d.store.notes {
		noteID = k.String()
	}
	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, base, "", http.StatusOK, ""},
		{http.MethodPatch, base + "/" + noteID, `{"content":"el martes"}`, http.StatusOK, ""},
		{http.MethodPatch, base + "/" + noteID, `{"content":""}`, http.StatusBadRequest, "invalid_content"},
		{http.MethodPatch, base + "/no-es-un-id", `{"content":"x"}`, http.StatusBadRequest, "invalid_note_id"},
		{http.MethodPatch, base + "/" + id.New().String(), `{"content":"x"}`, http.StatusNotFound, "note_not_found"},
		{http.MethodPost, base, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodGet, "/conversations/no-es-un-id/notes", "", http.StatusBadRequest, "invalid_conversation_id"},
		{http.MethodGet, "/conversations/" + id.New().String() + "/notes", "", http.StatusNotFound, "conversation_not_found"},
		{http.MethodDelete, base + "/" + noteID, "", http.StatusNoContent, ""},
	} {
		rec := send(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
	}

	other := newDesk()
	theirs := other.open(&otherOperator)
	noteOfOther := other.note(theirs, otherOperator)
	rec := send(mount(enums.OperatorRoleAdmin, other), http.MethodDelete, "/conversations/"+theirs.String()+"/notes/"+noteOfOther.String(), "")
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "not_the_author" {
		t.Fatalf("an admin deleting another's note: got %d - body %s, want 403 not_the_author", rec.Code, rec.Body)
	}
}
