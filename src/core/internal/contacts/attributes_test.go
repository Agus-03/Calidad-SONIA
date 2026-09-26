package contacts

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// ADR-0047, section 2.7: the card carries what F31 found, and the three sensitive keys follow the rule of the four encrypted
// columns — the operator does not get them at all, and the supervisor and the admin do (ADR-0032, regla 4).
func TestTheExtractedSensitiveDataFollowTheRuleOfTheFourFields(t *testing.T) {
	store, contact := seededContact()
	confidence := 0.9
	store.attributes = map[uuid.UUID][]Attribute{contact.ID: {
		{Key: "dni", Value: "30123456", Source: enums.AttributeSourceLlm, CreatedAt: time.Now()},
		{Key: "first_name", Value: "Lucía", Source: enums.AttributeSourceLlm, Confidence: &confidence, CreatedAt: time.Now()},
	}}

	for _, tc := range []struct {
		role    enums.OperatorRole
		wantDNI bool
	}{
		{enums.OperatorRoleOperator, false},
		{enums.OperatorRoleSupervisor, true},
		{enums.OperatorRoleAdmin, true},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			rec := call(mount(tc.role, store), http.MethodGet, "/contacts/"+contact.ID.String(), "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET the card: got %d - %s", rec.Code, rec.Body)
			}
			var body struct {
				Attributes []struct {
					Key    string `json:"key"`
					Value  string `json:"value"`
					Source string `json:"source"`
				} `json:"attributes"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("the card is not json: %v", err)
			}
			keys := map[string]string{}
			for _, a := range body.Attributes {
				keys[a.Key] = a.Value
			}
			if keys["first_name"] != "Lucía" {
				t.Fatalf("attributes = %v, want the name every role reads", body.Attributes)
			}
			if _, has := keys["dni"]; has != tc.wantDNI || strings.Contains(rec.Body.String(), "30123456") != tc.wantDNI {
				t.Fatalf("the extracted DNI is on the card: %v, want %v - body %s", has, tc.wantDNI, rec.Body)
			}
		})
	}
}

// A contact nobody talked to has an empty list, not a missing one: the frontend draws the section either way.
func TestACardWithNothingExtractedCarriesAnEmptyList(t *testing.T) {
	store, contact := seededContact()

	rec := call(mount(enums.OperatorRoleOperator, store), http.MethodGet, "/contacts/"+contact.ID.String(), "")
	if !strings.Contains(rec.Body.String(), `"attributes":[]`) {
		t.Fatalf("the card = %s, want an empty attributes list", rec.Body)
	}
}
