package labels

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

var (
	testAccount  = id.New()
	testOperator = id.New()
)

// mount builds the real router, because what these cases assert is the roles declared in routes.go and those only exist there.
func mount(role enums.OperatorRole, store *stubStore) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(NewService(store, nil), nil)},
		TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
		ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
			return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
		},
	})
}

func call(engine http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, "/api/v1"+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func seeded(name string) Label {
	return Label{ID: id.New(), AccountID: testAccount, Name: name, Color: "#112233", CreatedByOperatorID: testOperator}
}

// TestOnlyTheAdminWritesTheCatalogue is the case this task exists for: reading is the three roles, writing is the admin (F19, ADR-0032).
func TestOnlyTheAdminWritesTheCatalogue(t *testing.T) {
	writes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"crear", http.MethodPost, "/labels", `{"name":"Nueva","color":"#112233"}`},
		{"renombrar", http.MethodPatch, "/labels/", `{"name":"Otra","color":"#112233"}`},
		{"borrar", http.MethodDelete, "/labels/", ""},
	}

	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			existing := seeded("Ventas")

			// Reading is allowed for every role, and it is the half that would be easy to close by accident.
			if rec := call(mount(role, newStubStore(existing)), http.MethodGet, "/labels", ""); rec.Code != http.StatusOK {
				t.Fatalf("GET /labels as %s: got %d, want 200 - body %s", role, rec.Code, rec.Body)
			}

			wantWrite := http.StatusForbidden
			if role == enums.OperatorRoleAdmin {
				wantWrite = 0 // any success: the codes differ per verb and are pinned in their own cases
			}

			for _, w := range writes {
				path := w.path
				if strings.HasSuffix(path, "/") {
					path += existing.ID.String()
				}
				rec := call(mount(role, newStubStore(existing)), w.method, path, w.body)

				if wantWrite == http.StatusForbidden {
					if rec.Code != http.StatusForbidden {
						t.Fatalf("%s as %s: got %d, want 403 - body %s", w.name, role, rec.Code, rec.Body)
					}
					continue
				}
				if rec.Code >= http.StatusBadRequest {
					t.Fatalf("%s as admin: got %d, want a success - body %s", w.name, rec.Code, rec.Body)
				}
			}
		})
	}
}

// TestTheAdminGetsTheStatusThatMatchesTheVerb pins what the previous case deliberately does not: 201 on create, 200 on rename, 204 on delete.
func TestTheAdminGetsTheStatusThatMatchesTheVerb(t *testing.T) {
	existing := seeded("Ventas")
	store := newStubStore(existing)
	engine := mount(enums.OperatorRoleAdmin, store)

	rec := call(engine, http.MethodPost, "/labels", `{"name":"Urgente","color":"#445566"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /labels: got %d, want 201 - body %s", rec.Code, rec.Body)
	}
	var created labelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("the created label is not json: %v", err)
	}
	if created.ID == "" || created.CreatedByOperatorID != testOperator.String() {
		t.Fatalf("the created label is %+v, want an id and the operator of the actor", created)
	}

	rec = call(engine, http.MethodPatch, "/labels/"+existing.ID.String(), `{"name":"Ventas 2026","color":"#778899"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /labels/:id: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	rec = call(engine, http.MethodDelete, "/labels/"+existing.ID.String(), "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /labels/:id: got %d, want 204 - body %s", rec.Code, rec.Body)
	}
}

// TestARepeatedNameIsAConflictAndNotAFailure is the branch that would otherwise reach the caller as a 500 with no way to act on it.
func TestARepeatedNameIsAConflictAndNotAFailure(t *testing.T) {
	engine := mount(enums.OperatorRoleAdmin, newStubStore(seeded("Ventas")))

	rec := call(engine, http.MethodPost, "/labels", `{"name":"ventas","color":"#112233"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST of a name taken in another casing: got %d, want 409 - body %s", rec.Code, rec.Body)
	}
	if code := codeOf(t, rec); code != "label_name_taken" {
		t.Fatalf("the error code is %q, want label_name_taken", code)
	}
}

// TestTheBadRequestsAnswerACodeTheFrontendCanBranchOn keeps the four refusals apart, because "invalid" alone cannot be shown to a user.
func TestTheBadRequestsAnswerACodeTheFrontendCanBranchOn(t *testing.T) {
	existing := seeded("Ventas")

	for _, tc := range []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode string
	}{
		{"sin color", http.MethodPost, "/labels", `{"name":"Nueva"}`, "invalid_body"},
		{"color libre", http.MethodPost, "/labels", `{"name":"Nueva","color":"rojo"}`, "invalid_color"},
		{"nombre en blanco", http.MethodPost, "/labels", `{"name":"   ","color":"#112233"}`, "invalid_name"},
		{"id que no es uuid", http.MethodDelete, "/labels/no-soy-uuid", "", "invalid_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(mount(enums.OperatorRoleAdmin, newStubStore(existing)), tc.method, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			if code := codeOf(t, rec); code != tc.wantCode {
				t.Fatalf("the error code is %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestAPagePastTheEndIsEmptyAndNotMissing is the decision written down: nothing here is not the same as this does not exist.
func TestAPagePastTheEndIsEmptyAndNotMissing(t *testing.T) {
	engine := mount(enums.OperatorRoleOperator, newStubStore(seeded("Ventas"), seeded("Urgente")))

	rec := call(engine, http.MethodGet, "/labels?page=9&per_page=20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET a page past the end: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var page pageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the page is not json: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("the items are %+v, want an empty list and not null", page.Items)
	}
	if page.Total != 2 || page.Page != 9 || page.PerPage != 20 {
		t.Fatalf("the page is %+v, want the real total and the page asked for", page)
	}
}

// TestPerPageIsCappedSoTheCallerCannotChooseTheQuery protects the database from a page size nobody would page through.
func TestPerPageIsCappedSoTheCallerCannotChooseTheQuery(t *testing.T) {
	engine := mount(enums.OperatorRoleSupervisor, newStubStore(seeded("Ventas")))

	rec := call(engine, http.MethodGet, "/labels?per_page=100000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET with a huge per_page: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var page pageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the page is not json: %v", err)
	}
	if page.PerPage != maxPerPage {
		t.Fatalf("per_page came back as %d, want it capped at %d", page.PerPage, maxPerPage)
	}
}

// TestWritingALabelThatIsNotThereIsA404 walks the not found branch over HTTP, which is the one a silent success would hide.
func TestWritingALabelThatIsNotThereIsA404(t *testing.T) {
	missing := id.New().String()

	for _, tc := range []struct {
		name   string
		method string
		body   string
	}{
		{"renombrar", http.MethodPatch, `{"name":"Otra","color":"#112233"}`},
		{"borrar", http.MethodDelete, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(mount(enums.OperatorRoleAdmin, newStubStore(seeded("Ventas"))), tc.method, "/labels/"+missing, tc.body)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("got %d, want 404 - body %s", rec.Code, rec.Body)
			}
			if code := codeOf(t, rec); code != "label_not_found" {
				t.Fatalf("the error code is %q, want label_not_found", code)
			}
		})
	}
}

// TestAMalformedPageIsRefusedInsteadOfGuessed keeps a typo from silently becoming page one, which would look like data going missing.
func TestAMalformedPageIsRefusedInsteadOfGuessed(t *testing.T) {
	for _, tc := range []struct {
		query    string
		wantCode string
	}{
		{"?page=cero", "invalid_page"},
		{"?page=0", "invalid_page"},
		{"?page=-3", "invalid_page"},
		{"?per_page=muchas", "invalid_per_page"},
		{"?per_page=0", "invalid_per_page"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := call(mount(enums.OperatorRoleOperator, newStubStore(seeded("Ventas"))), http.MethodGet, "/labels"+tc.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			if code := codeOf(t, rec); code != tc.wantCode {
				t.Fatalf("the error code is %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestRenamingWithABrokenBodyIsRefusedBeforeTheService is the bind branch of the other write verb, which the create cases do not reach.
func TestRenamingWithABrokenBodyIsRefusedBeforeTheService(t *testing.T) {
	existing := seeded("Ventas")
	rec := call(mount(enums.OperatorRoleAdmin, newStubStore(existing)), http.MethodPatch, "/labels/"+existing.ID.String(), `{"color":"#112233"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH with no name: got %d, want 400 - body %s", rec.Code, rec.Body)
	}
	if code := codeOf(t, rec); code != "invalid_body" {
		t.Fatalf("the error code is %q, want invalid_body", code)
	}
}

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	// The envelope is what Fail writes: the code the frontend switches on lives under "error", not at the top level.
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error body is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code
}
