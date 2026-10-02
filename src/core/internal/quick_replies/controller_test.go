package quickreplies

import (
	"context"
	"encoding/json"
	"errors"
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

// TestOnlyTheAdminWritesTheQuickReplies: F09 says the admin loads them and the operators pick them, so reading is the three roles.
func TestOnlyTheAdminWritesTheQuickReplies(t *testing.T) {
	existing := reply(testAccount, "Horarios", true)
	for _, role := range []enums.OperatorRole{enums.OperatorRoleOperator, enums.OperatorRoleSupervisor} {
		engine := mount(role, newStore(existing))
		if rec := call(engine, http.MethodGet, "/quick-replies", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s reading: got %d, want 200", role, rec.Code)
		}
		for _, w := range []struct{ method, path, body string }{
			{http.MethodPost, "/quick-replies", `{"title":"Nueva","content":"texto"}`},
			{http.MethodPatch, "/quick-replies/" + existing.ID.String(), `{"active":false}`},
			{http.MethodDelete, "/quick-replies/" + existing.ID.String(), ""},
		} {
			if rec := call(engine, w.method, w.path, w.body); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s by %s: got %d, want 403", w.method, w.path, role, rec.Code)
			}
		}
	}
}

// TestTheQuickReplyRoutesAnswerWithTheirCodes walks the four routes as the admin, because a wrong path or method compiles.
func TestTheQuickReplyRoutesAnswerWithTheirCodes(t *testing.T) {
	existing := reply(testAccount, "Horarios", true)
	engine := mount(enums.OperatorRoleAdmin, newStore(existing))

	rec := call(engine, http.MethodPost, "/quick-replies", `{"title":"Precios","content":"La lista va adjunta."}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"title":"Precios","content":"La lista va adjunta.","active":true`) {
		t.Fatalf("POST: got %d - body %s, want 201 with the reply active", rec.Code, rec.Body)
	}
	var created replyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("the reply is not json: %v", err)
	}

	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
	}{
		{http.MethodGet, "/quick-replies?q=prec&page=1&per_page=500", "", http.StatusOK, ""},
		{http.MethodGet, "/quick-replies?page=0", "", http.StatusBadRequest, "invalid_page"},
		{http.MethodGet, "/quick-replies?per_page=x", "", http.StatusBadRequest, "invalid_per_page"},
		{http.MethodPost, "/quick-replies", `{"title":"Sin texto"}`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPost, "/quick-replies", `{"title":" ","content":"texto"}`, http.StatusBadRequest, "invalid_title"},
		{http.MethodPost, "/quick-replies", `{"title":"Vacía","content":"  "}`, http.StatusBadRequest, "invalid_content"},
		{http.MethodPost, "/quick-replies", `{"title":"horarios","content":"texto"}`, http.StatusConflict, "quick_reply_title_taken"},
		{http.MethodPatch, "/quick-replies/" + created.ID, `{"active":false}`, http.StatusOK, ""},
		{http.MethodPatch, "/quick-replies/" + created.ID, `no`, http.StatusBadRequest, "invalid_body"},
		{http.MethodPatch, "/quick-replies/x", `{}`, http.StatusBadRequest, "invalid_id"},
		{http.MethodPatch, "/quick-replies/" + id.New().String(), `{}`, http.StatusNotFound, "quick_reply_not_found"},
		{http.MethodDelete, "/quick-replies/" + created.ID, "", http.StatusNoContent, ""},
		{http.MethodDelete, "/quick-replies/" + created.ID, "", http.StatusNotFound, "quick_reply_not_found"},
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

// TestTheListCapsThePageAndReportsTheTotal, so per_page=500 is not a query the caller can ask for.
func TestTheListCapsThePageAndReportsTheTotal(t *testing.T) {
	store := newStore()
	for i := 0; i < maxPerPage+5; i++ {
		q := reply(testAccount, id.New().String(), true)
		store.rows[q.ID] = q
	}
	rec := call(mount(enums.OperatorRoleOperator, store), http.MethodGet, "/quick-replies?per_page=500", "")
	var page pageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != maxPerPage || page.Total != maxPerPage+5 || page.PerPage != maxPerPage {
		t.Fatalf("got %d items of %d with per_page %d and %v, want the ceiling", len(page.Items), page.Total, page.PerPage, err)
	}
}

// TestAFailedReadIsA500 lets an error the package does not know fall through instead of dressing it as a 4xx.
func TestAFailedReadIsA500(t *testing.T) {
	store := newStore()
	store.err = errors.New("the database is gone")
	if rec := call(mount(enums.OperatorRoleOperator, store), http.MethodGet, "/quick-replies", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

// TestTheHandlersRefuseWithoutAnActor guards the wiring the same way the other packages do.
func TestTheHandlersRefuseWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := NewController(NewService(newStore(), nil), nil)
	for _, handler := range []gin.HandlerFunc{c.list, c.create, c.update, c.remove} {
		rec := httptest.NewRecorder()
		gc, _ := gin.CreateTestContext(rec)
		gc.Request = httptest.NewRequest(http.MethodGet, "/quick-replies", nil)
		handler(gc)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401 without an actor", rec.Code)
		}
	}
}
