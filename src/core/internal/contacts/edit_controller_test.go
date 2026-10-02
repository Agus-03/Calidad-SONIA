package contacts

import (
	"net/http"
	"testing"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

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
