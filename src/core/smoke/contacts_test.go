package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// Short and well under the 72 bytes bcrypt reads. The three operators share it because what is under test is the role, not the secret.
const walkerPassword = "la-del-recorrido-de-punta-a-punta"

// The value the whole case hunts for: it is searched by hand in the raw body an operator receives, so a leak under another name
// is caught even when the four keys are absent (ADR-0032, regla 4).
const walkerCBU = "0170099220000067797151"

// world is what the walk needs in the database before the first login, and what has to disappear after the last one.
type world struct {
	byRole     map[enums.OperatorRole]string
	reachable  string
	outOfReach string
	label      string
}

// QA-003: a fixture can be on any page after an earlier load campaign.
func contactIDsAcrossPages(t *testing.T, core, token string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for page := 1; ; page++ {
		status, body := call(t, core, http.MethodGet, fmt.Sprintf("/api/v1/contacts?per_page=100&page=%d", page), token, "")
		if status != http.StatusOK {
			t.Fatalf("contact page %d: HTTP %d", page, status)
		}
		var result struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total   int `json:"total"`
			PerPage int `json:"per_page"`
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil || result.PerPage < 1 {
			t.Fatalf("invalid contact page %d: %v", page, err)
		}
		for _, item := range result.Items {
			found[item.ID] = true
		}
		if page*result.PerPage >= result.Total {
			return found
		}
	}
}

func TestContactPaginationVisitsAllPages(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(`{"items":[{"id":"first"}],"total":2,"per_page":1}`))
		} else {
			_, _ = w.Write([]byte(`{"items":[{"id":"second"}],"total":2,"per_page":1}`))
		}
	}))
	defer server.Close()

	found := contactIDsAcrossPages(t, server.URL, "test-token")

	if requests != 2 || len(found) != 2 || !found["first"] || !found["second"] {
		t.Fatalf("requests=%d, contacts=%v; want both pages", requests, found)
	}
}

// TestTheThreeRolesWalkTheContactBookThroughTheRealProcess is the only case that crosses everything at once: a real login, a
// real JWT, the middleware resolving the operator against its row, the account travelling from the token into the scope, and
// the cipher reading a key that lives in the environment and not in a test.
//
// Every one of those is already covered by a package test against a router built in memory. None of those can fail if the piece
// is simply not plugged into the process, and this one can.
func TestTheThreeRolesWalkTheContactBookThroughTheRealProcess(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)

	w := buildWorld(ctx, t, core, pool)

	tokens := map[enums.OperatorRole]string{}
	for role, email := range w.byRole {
		tokens[role] = login(t, core, email, walkerPassword)
	}

	t.Run("los cuatro campos no viajan al operador", func(t *testing.T) {
		card := "/api/v1/contacts/" + w.reachable

		status, body := call(t, core, http.MethodGet, card, tokens[enums.OperatorRoleOperator], "")
		if status != http.StatusOK {
			t.Fatalf("the operator asking for a contact it reaches: got %d, want 200 - %s", status, body)
		}
		for _, field := range []string{"dni", "cuit", "cuil", "cbu"} {
			if _, present := keysOf(t, body)[field]; present {
				t.Fatalf("the key %q reached an operator through the real process - %s", field, body)
			}
		}
		if strings.Contains(body, walkerCBU) {
			t.Fatalf("the CBU is in the operator's body under some other name - %s", body)
		}

		// And it does reach the two roles the decision names, or the assertion above would pass on an empty column.
		for _, role := range []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin} {
			status, body := call(t, core, http.MethodGet, card, tokens[role], "")
			if status != http.StatusOK {
				t.Fatalf("%s asking for the card: got %d, want 200 - %s", role, status, body)
			}
			if !strings.Contains(body, walkerCBU) {
				t.Fatalf("the CBU did not reach a %s, so the case above proves nothing - %s", role, body)
			}
		}
	})

	t.Run("el operador alcanza solo los contactos de sus conversaciones", func(t *testing.T) {
		operatorContacts := contactIDsAcrossPages(t, core, tokens[enums.OperatorRoleOperator])
		if !operatorContacts[w.reachable] {
			t.Fatal("the operator lost the contact of its own conversation")
		}
		if operatorContacts[w.outOfReach] {
			t.Fatal("the operator reached a contact nobody assigned to it")
		}

		adminContacts := contactIDsAcrossPages(t, core, tokens[enums.OperatorRoleAdmin])
		if !adminContacts[w.reachable] || !adminContacts[w.outOfReach] {
			t.Fatal("the admin does not see both fixture contacts across the contact book pages")
		}
	})

	t.Run("el operador edita dos campos y el resto se le niega por nombre", func(t *testing.T) {
		card := "/api/v1/contacts/" + w.reachable
		operator := tokens[enums.OperatorRoleOperator]

		status, body := call(t, core, http.MethodPatch, card, operator, `{"name":"Corregido"}`)
		if status != http.StatusOK {
			t.Fatalf("the operator correcting a name: got %d, want 200 - %s", status, body)
		}

		status, body = call(t, core, http.MethodPatch, card, operator, `{"notes":"me lo quedo yo"}`)
		if status != http.StatusForbidden {
			t.Fatalf("the operator writing the notes: got %d, want 403 - %s", status, body)
		}
		if code, details := refusal(t, body); code != "field_not_writable" || details["field"] != "notes" {
			t.Fatalf("the refusal is %q on %q, want field_not_writable naming notes - %s", code, details["field"], body)
		}

		// A field that is not a contact at all is the other answer, and the order between the two is what makes a typo readable.
		status, body = call(t, core, http.MethodPatch, card, operator, `{"apodo":"Anita"}`)
		if status != http.StatusBadRequest {
			t.Fatalf("the operator sending a field nobody declared: got %d, want 400 - %s", status, body)
		}
		if code, _ := refusal(t, body); code != "unknown_field" {
			t.Fatalf("the refusal is %q, want unknown_field - %s", code, body)
		}
	})

	t.Run("etiquetar y segmentar", func(t *testing.T) {
		operator := tokens[enums.OperatorRoleOperator]

		status, body := call(t, core, http.MethodPost,
			"/api/v1/contacts/"+w.reachable+"/labels", operator, `{"label_id":"`+w.label+`"}`)
		if status != http.StatusOK {
			t.Fatalf("the operator tagging a contact it reaches: got %d, want 200 - %s", status, body)
		}

		segment := "/api/v1/contacts?per_page=100&label_id=" + w.label
		_, body = call(t, core, http.MethodGet, segment, tokens[enums.OperatorRoleAdmin], "")
		if !listed(t, body, w.reachable) || listed(t, body, w.outOfReach) {
			t.Fatalf("the segment is not the contacts carrying the label - %s", body)
		}

		// The same segment read by the operator: the filter narrows on top of the reach and never instead of it.
		_, body = call(t, core, http.MethodGet, segment, operator, "")
		if !listed(t, body, w.reachable) {
			t.Fatalf("the operator lost its own tagged contact in the segment - %s", body)
		}

		status, body = call(t, core, http.MethodDelete,
			"/api/v1/contacts/"+w.reachable+"/labels/"+w.label, operator, "")
		if status != http.StatusOK {
			t.Fatalf("the operator untagging: got %d, want 200 - %s", status, body)
		}
		_, body = call(t, core, http.MethodGet, segment, tokens[enums.OperatorRoleAdmin], "")
		if listed(t, body, w.reachable) {
			t.Fatalf("the contact stayed in the segment after the tag was removed - %s", body)
		}
	})

	t.Run("las rutas que un rol entero tiene negadas", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			method  string
			path    string
			body    string
			refused []enums.OperatorRole
		}{
			{"escribir los cuatro", http.MethodPut, "/api/v1/contacts/" + w.reachable + "/sensitive", `{"dni":"36415782"}`,
				[]enums.OperatorRole{enums.OperatorRoleOperator}},
			{"dar de baja", http.MethodDelete, "/api/v1/contacts/" + w.outOfReach, "",
				[]enums.OperatorRole{enums.OperatorRoleOperator, enums.OperatorRoleSupervisor}},
		} {
			for _, role := range tc.refused {
				status, body := call(t, core, tc.method, tc.path, tokens[role], tc.body)
				if status != http.StatusForbidden {
					t.Fatalf("%s as %s: got %d, want 403 - %s", tc.name, role, status, body)
				}
			}
		}
	})

	// The guard that keeps every 200 above from being the middleware simply not running.
	t.Run("sin token no se entra", func(t *testing.T) {
		for _, token := range []string{"", "no-soy-un-jwt"} {
			if status, _ := call(t, core, http.MethodGet, "/api/v1/contacts", token, ""); status != http.StatusUnauthorized {
				t.Fatalf("with token %q: got %d, want 401", token, status)
			}
		}
	})
}

// buildWorld writes the rows nothing in the system creates yet and then builds the rest through the API, which is the point:
// what can be set up over HTTP is set up over HTTP, so the walk exercises it instead of assuming it.
//
// The conversation is the exception. Assignment to an operator is Fase 5, so there is no endpoint that creates one, and the
// reach of ADR-0032 regla 2 is read off exactly that column.
func buildWorld(ctx context.Context, t *testing.T, core string, pool db.Pool) world {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("the demo rows are missing: load src/core/seeds/0001_demo.sql first")
	}
	var inbox uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM inboxes LIMIT 1`).Scan(&inbox); err != nil {
		t.Skip("no inbox in the demo rows: load src/core/seeds/0001_demo.sql first")
	}

	hash, err := operators.Hash(walkerPassword)
	if err != nil {
		t.Fatalf("hashing the password: %v", err)
	}

	// Armed before the first row exists and not after the last one: a failure halfway through this function would otherwise
	// leave rows behind, and the run after it would trip over the unique index on the phone instead of reporting the real fault.
	w := &world{byRole: map[enums.OperatorRole]string{}}
	ids := map[enums.OperatorRole]uuid.UUID{}
	t.Cleanup(func() { tearDownWorld(t, pool, w, ids) })
	for _, role := range enums.OperatorRoleValues {
		operatorID := id.New()
		email := "walker-" + operatorID.String() + "@coop.test"
		_, err := pool.Exec(ctx, `
			INSERT INTO operators (id, account_id, name, lastname, email, password_hash, role, active)
			VALUES ($1, $2, 'Recorrido', $3, $4, $5, $6, true)`,
			operatorID, account, string(role), email, hash, string(role))
		if err != nil {
			t.Fatalf("seeding the %s: %v", role, err)
		}
		w.byRole[role], ids[role] = email, operatorID
	}

	admin := login(t, core, w.byRole[enums.OperatorRoleAdmin], walkerPassword)

	// Created through the API and not with an INSERT, because the encryption of the four is part of what this case is checking:
	// a CBU written by hand into the column would come back as a decryption failure and not as a value.
	w.reachable = createContact(t, core, admin, "Alcanzada", "Asignada", walkerCBU)
	w.outOfReach = createContact(t, core, admin, "Ajena", "SinAsignar", "")
	w.label = createLabel(t, core, admin, "Recorrido "+id.New().String())

	_, err = pool.Exec(ctx, `
		INSERT INTO conversations (id, contact_id, inbox_id, operator_id, status, mode, last_inbound_at, window_expires_at)
		VALUES ($1, $2, $3, $4, 'active', 'autonomous', now(), now() + interval '24 hours')`,
		id.New(), uuid.MustParse(w.reachable), inbox, ids[enums.OperatorRoleOperator])
	if err != nil {
		t.Fatalf("assigning the conversation: %v", err)
	}

	return *w
}

// Nothing here runs in a transaction, because the process under test has its own. The rows are taken out by hand, children
// first, or the foreign keys refuse and the next run starts on top of this one.
//
// Every field is read as optional: this also runs after a setup that failed partway, which is exactly when it matters most.
func tearDownWorld(t *testing.T, pool db.Pool, w *world, ids map[enums.OperatorRole]uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	contacts := written(w.reachable, w.outOfReach)
	labelIDs := written(w.label)
	operatorIDs := make([]uuid.UUID, 0, len(ids))
	for _, one := range ids {
		operatorIDs = append(operatorIDs, one)
	}

	for _, step := range []struct {
		what string
		sql  string
		args []any
	}{
		{"the tags", `DELETE FROM contact_labels WHERE contact_id = ANY($1)`, []any{contacts}},
		{"the conversations", `DELETE FROM conversations WHERE contact_id = ANY($1)`, []any{contacts}},
		{"the contacts", `DELETE FROM contacts WHERE id = ANY($1)`, []any{contacts}},
		{"the label", `DELETE FROM labels WHERE id = ANY($1)`, []any{labelIDs}},
		{"the operators", `DELETE FROM operators WHERE id = ANY($1)`, []any{operatorIDs}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Errorf("cleaning up %s: %v", step.what, err)
		}
	}
}

// written keeps the ids that got as far as being created, so a teardown after a half built world deletes those and not a panic.
func written(maybe ...string) []uuid.UUID {
	kept := []uuid.UUID{}
	for _, one := range maybe {
		if parsed, err := uuid.Parse(one); err == nil {
			kept = append(kept, parsed)
		}
	}
	return kept
}

// onlyDigits is what the phone column holds and what Meta sends as wa_id: the dashes of a uuid are not part of a number.
func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func createContact(t *testing.T, core, token, name, lastname, cbu string) string {
	t.Helper()

	// Every digit of a fresh uuid and not a slice of one: the phone carries a unique index, and two contacts built in the same
	// millisecond collide on anything shorter.
	phone := "549351" + onlyDigits(id.New().String())
	body := fmt.Sprintf(
		`{"phone_whatsapp":%q,"name":%q,"lastname":%q,"type":"client","source_channel":"direct","cbu":%q}`,
		phone, name, lastname, cbu)

	status, answer := call(t, core, http.MethodPost, "/api/v1/contacts", token, body)
	if status != http.StatusCreated {
		t.Fatalf("creating the contact %s: got %d, want 201 - %s", lastname, status, answer)
	}
	return stringField(t, answer, "id")
}

func createLabel(t *testing.T, core, token, name string) string {
	t.Helper()

	status, answer := call(t, core, http.MethodPost, "/api/v1/labels", token,
		fmt.Sprintf(`{"name":%q,"color":"#ff8800"}`, name))
	if status != http.StatusCreated {
		t.Fatalf("creating the label: got %d, want 201 - %s", status, answer)
	}
	return stringField(t, answer, "id")
}

// login goes through /auth/login and not through a token minted here, because the point is that the process issues one this
// same process then accepts (ADR-0030).
func login(t *testing.T, core, email, password string) string {
	t.Helper()

	status, answer := call(t, core, http.MethodPost, "/auth/login", "",
		fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	if status != http.StatusOK {
		t.Fatalf("logging %s in: got %d, want 200 - %s", email, status, answer)
	}
	return stringField(t, answer, "token")
}

func call(t *testing.T, core, method, path, token, body string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(method, core+path, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = res.Body.Close() }()

	answer, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading the answer of %s %s: %v", method, path, err)
	}
	return res.StatusCode, string(answer)
}

func keysOf(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()

	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &keys); err != nil {
		t.Fatalf("the answer is not a json object: %v - %s", err, body)
	}
	return keys
}

func stringField(t *testing.T, body, field string) string {
	t.Helper()

	var value string
	if err := json.Unmarshal(keysOf(t, body)[field], &value); err != nil {
		t.Fatalf("the answer carries no %s: %v - %s", field, err, body)
	}
	return value
}

// listed reads ids out of a page, which is the only thing these cases assert about a listing: whether a contact is in it.
func listed(t *testing.T, body, contactID string) bool {
	t.Helper()

	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the listing is not json: %v - %s", err, body)
	}
	for _, one := range page.Items {
		if one.ID == contactID {
			return true
		}
	}
	return false
}

// refusal unwraps the envelope Fail writes, so a case asserts the stable code and never the sentence next to it.
func refusal(t *testing.T, body string) (string, map[string]string) {
	t.Helper()

	var envelope struct {
		Error struct {
			Code    string            `json:"code"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("the refusal is not json: %v - %s", err, body)
	}
	return envelope.Error.Code, envelope.Error.Details
}
