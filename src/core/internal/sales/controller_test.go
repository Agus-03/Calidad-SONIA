package sales

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount  = id.New()
	testOperator = id.New()
)

// mount builds the real router, because the paths and the roles only exist in routes.go.
func mount(role enums.OperatorRole, store *stubStore) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(NewService(store, store, nil), nil)},
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

// TestTheSaleRoutesAnswerWithTheirCodes walks the four routes, because a wrong path or method compiles (ADR-0059).
func TestTheSaleRoutesAnswerWithTheirCodes(t *testing.T) {
	store := newStore()
	mine, pooled := Conversation{ID: id.New(), OperatorID: &testOperator}, Conversation{ID: id.New()}
	store.conversations[mine.ID], store.conversations[pooled.ID] = mine, pooled
	engine := mount(enums.OperatorRoleOperator, store)
	base := "/conversations/" + mine.ID.String() + "/sales"

	rec := call(engine, http.MethodPost, base, `{"amount":"1234.50"}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"amount":"1234.50","currency":"ARS","source_channel":"direct"`) {
		t.Fatalf("POST: got %d - body %s, want 201 with the amount as written", rec.Code, rec.Body)
	}
	var created saleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("the sale is not json: %v", err)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, base, "", http.StatusOK, ""},
		{http.MethodPost, base, `{"amount":1234.5}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPost, base, `{}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPost, base, `{"amount":"0"}`, http.StatusBadRequest, "invalid_amount"},
		{http.MethodPost, base, `{"amount":"10","currency":"pesos"}`, http.StatusBadRequest, "invalid_currency"},
		{http.MethodPost, "/conversations/x/sales", `{"amount":"10"}`, http.StatusBadRequest, "invalid_conversation_id"},
		{http.MethodPost, "/conversations/" + id.New().String() + "/sales", `{"amount":"10"}`, http.StatusNotFound, "conversation_not_found"},
		{http.MethodPost, "/conversations/" + pooled.ID.String() + "/sales", `{"amount":"10"}`, http.StatusConflict, "conversation_not_assigned"},
		{http.MethodPatch, "/sales/" + created.ID, `{"amount":"99.90"}`, http.StatusOK, ""},
		{http.MethodPatch, "/sales/" + created.ID, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPatch, "/sales/x", `{}`, http.StatusBadRequest, "invalid_sale_id"},
		{http.MethodPatch, "/sales/" + id.New().String(), `{}`, http.StatusNotFound, "sale_not_found"},
		{http.MethodDelete, "/sales/" + created.ID, "", http.StatusNoContent, ""},
	} {
		rec := call(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
	}
}

// TestAnotherVendorsSaleIsForbidden is the one refusal by role, which the frontend shows apart from a missing sale.
func TestAnotherVendorsSaleIsForbidden(t *testing.T) {
	store := newStore()
	sale := Sale{ID: id.New(), OperatorID: id.New(), Amount: "10", Currency: "ARS"}
	store.rows[sale.ID] = sale

	rec := call(mount(enums.OperatorRoleOperator, store), http.MethodDelete, "/sales/"+sale.ID.String(), "")
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "not_the_vendor" {
		t.Fatalf("got %d - body %s, want 403 not_the_vendor", rec.Code, rec.Body)
	}
}

// TestTheHandlersRefuseWithoutAnActor guards the wiring the same way the other packages do.
func TestTheHandlersRefuseWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newStore()
	c := NewController(NewService(store, store, nil), nil)
	for _, handler := range []gin.HandlerFunc{c.mark, c.ofConversation, c.update, c.remove} {
		rec := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(rec)
		gc.Request = httptest.NewRequest(http.MethodGet, "/sales", nil)
		handler(gc)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 without an actor", rec.Code)
		}
	}
}
