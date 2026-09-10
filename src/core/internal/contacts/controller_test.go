package contacts

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
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// seededContact is reachable by the test operator, so a refusal in these cases is about the role and never about the reach.
func seededContact() (*stubStore, Contact) {
	contact := Contact{
		ID: id.New(), AccountID: testAccount, PhoneWhatsapp: "5493511234567",
		Name: "Ana", Lastname: "Perez", Email: "ana@coop.test",
		Type: enums.ContactTypeClient, SourceChannel: enums.SourceChannelDirect,
		Sensitive: Sensitive{DNI: "36415782", CUIT: "20364157821", CUIL: "27364157823", CBU: "0170099220000067797151"},
	}
	store := newStubStore(contact)
	store.assign(testOperator, contact.ID)
	return store, contact
}

// TestTheFourFieldsDoNotTravelToAnOperator is the case this task exists for, and it asserts absence and not emptiness:
// a key present with "" would still be a field the frontend could learn to read, and a masked one would carry the value (ADR-0032, regla 4).
func TestTheFourFieldsDoNotTravelToAnOperator(t *testing.T) {
	store, contact := seededContact()

	rec := call(mount(enums.OperatorRoleOperator, store), http.MethodGet, "/contacts/"+contact.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET the card as operator: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the card is not json: %v", err)
	}
	for _, field := range []string{"dni", "cuit", "cuil", "cbu"} {
		if _, present := body[field]; present {
			t.Fatalf("the key %q is in the operator's card, want it absent and not empty - body %s", field, rec.Body)
		}
	}
	// The one that would catch a leak the key check cannot: the value itself, anywhere in the response.
	if strings.Contains(rec.Body.String(), "0170099220000067797151") {
		t.Fatalf("the CBU is in the body under some other name: %s", rec.Body)
	}

	for _, role := range []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin} {
		rec := call(mount(role, store), http.MethodGet, "/contacts/"+contact.ID.String(), "")
		if !strings.Contains(rec.Body.String(), "0170099220000067797151") {
			t.Fatalf("the CBU did not reach the %s, want it on the card - body %s", role, rec.Body)
		}
	}
}

// TestTheListingCarriesNoSensitiveFieldForAnyRole covers the other read: even an admin gets a page without them, because the query
// does not select them and a page of two hundred contacts must not be two hundred decryptions (ADR-0031).
func TestTheListingCarriesNoSensitiveFieldForAnyRole(t *testing.T) {
	store, _ := seededContact()

	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			rec := call(mount(role, store), http.MethodGet, "/contacts", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET the book: got %d, want 200 - body %s", rec.Code, rec.Body)
			}
			for _, field := range []string{`"dni"`, `"cbu"`, "0170099220000067797151"} {
				if strings.Contains(rec.Body.String(), field) {
					t.Fatalf("the listing carries %s - body %s", field, rec.Body)
				}
			}
		})
	}
}

// TestTheRoutesAdmitTheRolesTheDecisionNames walks the whole table, which is the only way a wrong Roles list gets caught: it compiles.
func TestTheRoutesAdmitTheRolesTheDecisionNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		body    string
		refused []enums.OperatorRole
	}{
		{"crear", http.MethodPost, "/contacts",
			`{"phone_whatsapp":"5493519999999","name":"Nuevo","lastname":"Contacto","type":"client","source_channel":"direct"}`,
			[]enums.OperatorRole{enums.OperatorRoleOperator}},
		{"escribir los cuatro", http.MethodPut, "/contacts/:id/sensitive", `{"dni":"36415782"}`,
			[]enums.OperatorRole{enums.OperatorRoleOperator}},
		{"dar de baja", http.MethodDelete, "/contacts/:id", "",
			[]enums.OperatorRole{enums.OperatorRoleOperator, enums.OperatorRoleSupervisor}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, role := range enums.OperatorRoleValues {
				store, contact := seededContact()
				path := strings.Replace(tc.path, ":id", contact.ID.String(), 1)
				rec := call(mount(role, store), tc.method, path, tc.body)

				if slicesContains(tc.refused, role) {
					if rec.Code != http.StatusForbidden {
						t.Fatalf("%s as %s: got %d, want 403 - body %s", tc.name, role, rec.Code, rec.Body)
					}
					continue
				}
				if rec.Code >= http.StatusBadRequest {
					t.Fatalf("%s as %s: got %d, want a success - body %s", tc.name, role, rec.Code, rec.Body)
				}
			}
		})
	}
}

// TestTheOperatorEditsTwoFieldsAndIsToldAboutTheRest is regla 14: the refusal names the field, because ignoring it in silence
// tells the vendor they saved something that was not saved.
func TestTheOperatorEditsTwoFieldsAndIsToldAboutTheRest(t *testing.T) {
	store, contact := seededContact()
	engine := mount(enums.OperatorRoleOperator, store)

	rec := call(engine, http.MethodPatch, "/contacts/"+contact.ID.String(), `{"name":"Ana Maria","lastname":"Perez"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH of the two it may write: got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	for _, tc := range []struct{ field, body string }{
		{"email", `{"email":"otra@coop.test"}`},
		{"type", `{"type":"provider"}`},
		{"notes", `{"notes":"algo"}`},
		{"opt_out", `{"opt_out":true}`},
		{"blocked", `{"blocked":true}`},
	} {
		t.Run(tc.field, func(t *testing.T) {
			rec := call(engine, http.MethodPatch, "/contacts/"+contact.ID.String(), tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("got %d, want 403 - body %s", rec.Code, rec.Body)
			}
			code, details := errorOf(t, rec)
			if code != "field_not_writable" {
				t.Fatalf("the error code is %q, want field_not_writable", code)
			}
			if details["field"] != tc.field {
				t.Fatalf("the refusal names %q, want %q", details["field"], tc.field)
			}
		})
	}

	// And the two it may write did land, so the refusals above are about the rule and not about a PATCH that never works.
	if store.rows[0].Name != "Ana Maria" {
		t.Fatalf("the name reads %q, want the correction the operator is allowed to make", store.rows[0].Name)
	}
}

// TestThePhoneAndTheOriginHaveNoWayIn is reglas 10 y 9 over HTTP: not refused as values, refused as names that do not exist.
func TestThePhoneAndTheOriginHaveNoWayIn(t *testing.T) {
	store, contact := seededContact()
	engine := mount(enums.OperatorRoleAdmin, store)

	for _, body := range []string{`{"phone_whatsapp":"5493510000000"}`, `{"source_channel":"qr"}`, `{"consent_given_at":"2026-01-01T00:00:00Z"}`} {
		rec := call(engine, http.MethodPatch, "/contacts/"+contact.ID.String(), body)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
			t.Fatalf("PATCH %s: got %d, want a refusal - body %s", body, rec.Code, rec.Body)
		}
	}
	if store.rows[0].PhoneWhatsapp != "5493511234567" || store.rows[0].SourceChannel != enums.SourceChannelDirect {
		t.Fatalf("an immutable moved: %q / %q", store.rows[0].PhoneWhatsapp, store.rows[0].SourceChannel)
	}
}

// TestAContactOutOfReachIsMissingAndNotForbidden protects what regla 2 decided: saying forbidden confirms the contact exists.
func TestAContactOutOfReachIsMissingAndNotForbidden(t *testing.T) {
	contact := Contact{
		ID: id.New(), AccountID: testAccount, PhoneWhatsapp: "5493517777777",
		Name: "Beto", Lastname: "Ajeno", Type: enums.ContactTypeClient, SourceChannel: enums.SourceChannelDirect,
	}
	// Assigned to somebody else, so the test operator has no conversation with it.
	store := newStubStore(contact)
	store.assign(id.New(), contact.ID)
	engine := mount(enums.OperatorRoleOperator, store)

	for _, tc := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPatch, `{"name":"Robado"}`},
	} {
		rec := call(engine, tc.method, "/contacts/"+contact.ID.String(), tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s of a contact out of reach: got %d, want 404 - body %s", tc.method, rec.Code, rec.Body)
		}
		if code, _ := errorOf(t, rec); code != "contact_not_found" {
			t.Fatalf("the error code is %q, want contact_not_found", code)
		}
	}
}

// TestTheBadRequestsAnswerACodeTheFrontendCanBranchOn keeps the refusals apart, because "invalid" alone cannot be shown to a user.
func TestTheBadRequestsAnswerACodeTheFrontendCanBranchOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode string
	}{
		{"tipo fuera del contrato", http.MethodPost, "/contacts",
			`{"phone_whatsapp":"5493518888888","name":"A","lastname":"B","type":"vip","source_channel":"direct"}`, "invalid_type"},
		{"origen fuera del contrato", http.MethodPost, "/contacts",
			`{"phone_whatsapp":"5493518888888","name":"A","lastname":"B","type":"client","source_channel":"tiktok"}`, "invalid_source_channel"},
		{"telefono sin digitos", http.MethodPost, "/contacts",
			`{"phone_whatsapp":"+-()","name":"A","lastname":"B","type":"client","source_channel":"direct"}`, "invalid_phone"},
		{"telefono repetido", http.MethodPost, "/contacts",
			`{"phone_whatsapp":"5493511234567","name":"A","lastname":"B","type":"client","source_channel":"direct"}`, "phone_taken"},
		{"campo que no existe", http.MethodPatch, "/contacts/:id", `{"apodo":"Anita"}`, "unknown_field"},
		{"nombre en blanco", http.MethodPatch, "/contacts/:id", `{"name":"   "}`, "invalid_name"},
		{"id que no es uuid", http.MethodGet, "/contacts/no-soy-uuid", "", "invalid_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, contact := seededContact()
			path := strings.Replace(tc.path, ":id", contact.ID.String(), 1)

			rec := call(mount(enums.OperatorRoleAdmin, store), tc.method, path, tc.body)
			if code, _ := errorOf(t, rec); code != tc.wantCode {
				t.Fatalf("got %d %q, want %q - body %s", rec.Code, code, tc.wantCode, rec.Body)
			}
		})
	}
}

// TestWritingTheFourReplacesAllOfThem is why the write takes them together: a partial write would mean a field could never be cleared.
func TestWritingTheFourReplacesAllOfThem(t *testing.T) {
	store, contact := seededContact()

	rec := call(mount(enums.OperatorRoleSupervisor, store), http.MethodPut,
		"/contacts/"+contact.ID.String()+"/sensitive", `{"dni":"11222333"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT the four: got %d, want 200 - body %s", rec.Code, rec.Body)
	}
	if store.rows[0].Sensitive != (Sensitive{DNI: "11222333"}) {
		t.Fatalf("the four now read %+v, want only the DNI", store.rows[0].Sensitive)
	}
}

// TestAMalformedPageIsRefusedInsteadOfGuessed keeps a typo from silently becoming page one, which would look like contacts going missing.
func TestAMalformedPageIsRefusedInsteadOfGuessed(t *testing.T) {
	store, _ := seededContact()

	for _, tc := range []struct{ query, wantCode string }{
		{"?page=cero", "invalid_page"},
		{"?page=0", "invalid_page"},
		{"?per_page=muchas", "invalid_per_page"},
		{"?per_page=-1", "invalid_per_page"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := call(mount(enums.OperatorRoleAdmin, store), http.MethodGet, "/contacts"+tc.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			if code, _ := errorOf(t, rec); code != tc.wantCode {
				t.Fatalf("the error code is %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestPerPageIsCappedSoTheCallerCannotChooseTheQuery protects the database from a page size nobody would page through.
func TestPerPageIsCappedSoTheCallerCannotChooseTheQuery(t *testing.T) {
	store, _ := seededContact()

	rec := call(mount(enums.OperatorRoleAdmin, store), http.MethodGet, "/contacts?per_page=100000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var page pageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the page is not json: %v", err)
	}
	if page.PerPage != maxPerPage {
		t.Fatalf("per_page came back as %d, want it capped at %d", page.PerPage, maxPerPage)
	}
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (string, map[string]string) {
	t.Helper()

	var body struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the error body is not json: %v - %s", err, rec.Body)
	}
	return body.Error.Code, body.Error.Details
}

func slicesContains(roles []enums.OperatorRole, role enums.OperatorRole) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
