package contacts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheThreeRolesTagAContact is regla 13 read from the other side of the router: defining the catalogue is the admin's, but
// putting one of its labels on a contact is everybody's.
func TestTheThreeRolesTagAContact(t *testing.T) {
	tag := labels.Label{ID: id.New(), AccountID: testAccount, Name: "Ventas", Color: "#ff8800"}

	for _, role := range enums.OperatorRoleValues {
		store, contact := seededContact()
		engine := mount(role, store, tag)

		rec := call(engine, http.MethodPost, "/contacts/"+contact.ID.String()+"/labels", `{"label_id":"`+tag.ID.String()+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("tag as %s: got %d, want 200 - body %s", role, rec.Code, rec.Body)
		}
		if names := labelNames(t, rec); len(names) != 1 || names[0] != "Ventas" {
			t.Fatalf("the card of a %s carries %v, want the tag just applied", role, names)
		}

		rec = call(engine, http.MethodDelete, "/contacts/"+contact.ID.String()+"/labels/"+tag.ID.String(), "")
		if rec.Code != http.StatusOK {
			t.Fatalf("untag as %s: got %d, want 200 - body %s", role, rec.Code, rec.Body)
		}
		if names := labelNames(t, rec); len(names) != 0 {
			t.Fatalf("the card of a %s still carries %v after the removal", role, names)
		}
	}
}

// TestALabelOfAnotherAccountIsMissingAndNotForbidden keeps the answer the same shape as the one for a contact out of reach:
// a 403 would confirm that the label exists somewhere.
func TestALabelOfAnotherAccountIsMissingAndNotForbidden(t *testing.T) {
	store, contact := seededContact()
	theirs := labels.Label{ID: id.New(), AccountID: id.New(), Name: "Ajena", Color: "#123456"}

	rec := call(mount(enums.OperatorRoleAdmin, store, theirs), http.MethodPost,
		"/contacts/"+contact.ID.String()+"/labels", `{"label_id":"`+theirs.ID.String()+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tagging with a label of another account: got %d, want 404 - body %s", rec.Code, rec.Body)
	}
	if code, _ := errorOf(t, rec); code != "label_not_found" {
		t.Fatalf("the code is %q, want label_not_found", code)
	}
}

// labelNames reads the names off a card, which is also what proves the card answers the state that is stored and not the one
// the handler assumed it left behind.
func labelNames(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()

	var card struct {
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("the card is not json: %v - body %s", err, rec.Body)
	}

	names := make([]string, 0, len(card.Labels))
	for _, l := range card.Labels {
		names = append(names, l.Name)
	}
	return names
}

// TestAMalformedLabelIdIsRefusedInsteadOfGuessed keeps the two label routes answering the same code the rest of the controller
// does, so the frontend has one branch for a bad identifier and not two.
func TestAMalformedLabelIdIsRefusedInsteadOfGuessed(t *testing.T) {
	store, contact := seededContact()
	engine := mount(enums.OperatorRoleAdmin, store)
	base := "/contacts/" + contact.ID.String() + "/labels"

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
		code   string
	}{
		{"un label_id que no es uuid", http.MethodPost, base, `{"label_id":"ventas"}`, "invalid_id"},
		{"sin label_id", http.MethodPost, base, `{}`, "invalid_body"},
		{"un label_id que no es uuid en la ruta", http.MethodDelete, base + "/ventas", "", "invalid_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(engine, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			if code, _ := errorOf(t, rec); code != tc.code {
				t.Fatalf("the code is %q, want %q", code, tc.code)
			}
		})
	}
}
