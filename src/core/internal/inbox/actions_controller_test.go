package inbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func patchMode(engine http.Handler, conversationID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+conversationID+"/mode", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestTheVendorSwitchesTheBotOfWhatItHolds is ADR-0037, regla 4, through the real router: the operator switches the bot of its own
// conversations, as F27 and Anexo B give it, and supervisor and admin the bot of any. What the operator cannot see does not exist,
// and the pool, which it can see, it has to take first; unless it is resolved, which taking it would not change.
func TestTheVendorSwitchesTheBotOfWhatItHolds(t *testing.T) {
	someoneElse := id.New()

	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		holder   *uuid.UUID
		resolved bool
		wantCode int
		want     string
	}{
		{"an operator switches the bot of its own conversation", enums.OperatorRoleOperator, &testOperator, false, http.StatusOK, ""},
		{"an operator is told to take the pool before switching its bot", enums.OperatorRoleOperator, nil, false, http.StatusConflict, "conversation_not_assigned"},
		{"an operator is told another's conversation does not exist", enums.OperatorRoleOperator, &someoneElse, false, http.StatusNotFound, "conversation_not_found"},
		{"a resolved one of the pool is told resolved, not sent to answer it and be refused again", enums.OperatorRoleOperator, nil, true, http.StatusConflict, "conversation_resolved"},
		{"another's resolved conversation still does not exist", enums.OperatorRoleOperator, &someoneElse, true, http.StatusNotFound, "conversation_not_found"},
		{"a supervisor switches any conversation of the team", enums.OperatorRoleSupervisor, &someoneElse, false, http.StatusOK, ""},
		{"an admin switches one of the pool too", enums.OperatorRoleAdmin, nil, false, http.StatusOK, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(tc.holder)
			if tc.resolved {
				target.Status = enums.ConversationStatusResolved
				d.store.targets[target.ConversationID] = target
			}

			rec := patchMode(mountDesk(tc.role, d), target.ConversationID.String(), `{"mode":"off"}`)
			if rec.Code != tc.wantCode {
				t.Fatalf("PATCH as %s: got %d, want %d - body %s", tc.role, rec.Code, tc.wantCode, rec.Body)
			}
			_, written := d.aggregate.modes[target.ConversationID]
			if tc.wantCode == http.StatusOK {
				if !written || !strings.Contains(rec.Body.String(), `"mode":"off"`) {
					t.Fatalf("written = %v, want the mode written and answered - body %s", written, rec.Body)
				}
				return
			}
			if written {
				t.Fatal("a refused switch still wrote the mode")
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEachModeRefusalHasItsOwnCode is what the frontend switches on when the toggle does not move (ADR-0035, reglas 11 y 12).
func TestEachModeRefusalHasItsOwnCode(t *testing.T) {
	onePooled := func(d *desk) string { return d.open(nil).ConversationID.String() }

	for _, tc := range []struct {
		name     string
		shape    func(*desk) string
		body     string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", func(*desk) string { return "no-es-un-id" }, `{"mode":"off"}`, http.StatusBadRequest, "invalid_conversation_id"},
		{"the body has no mode", onePooled, `{}`, http.StatusBadRequest, "invalid_body"},
		{"a word that is not a mode", onePooled, `{"mode":"robot"}`, http.StatusBadRequest, "invalid_mode"},
		{"the conversation does not exist", func(*desk) string { return id.New().String() }, `{"mode":"off"}`, http.StatusNotFound, "conversation_not_found"},
		{"no agent to turn on", func(d *desk) string {
			target := d.open(nil)
			target.AIAgentID, target.Mode = nil, enums.AgentModeOff
			d.store.targets[target.ConversationID] = target
			return target.ConversationID.String()
		}, `{"mode":"autonomous"}`, http.StatusConflict, "no_ai_agent"},
		{"an agent paused or removed by the admin (ADR-0038)", func(d *desk) string {
			target := d.open(nil)
			target.Mode, target.AIAgentAvailable = enums.AgentModeOff, false
			d.store.targets[target.ConversationID] = target
			return target.ConversationID.String()
		}, `{"mode":"autonomous"}`, http.StatusConflict, "ai_agent_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			conversationID := tc.shape(d)

			rec := patchMode(mountDesk(enums.OperatorRoleSupervisor, d), conversationID, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheModeHandlerRefusesWithoutAnActor guards the wiring the same way the other two handlers do.
func TestTheModeHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodPatch, "/conversations/x/mode", strings.NewReader(`{"mode":"off"}`))

	NewController(newDesk(&stubStore{}).service, nil).setMode(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func patchStarred(engine http.Handler, conversationID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/"+conversationID+"/starred", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestEveryRoleStarsWhatItReads walks the route with the three roles, because a wrong Roles list compiles: F25 names no role, and
// an operator refused here could not mark its own follow-ups (ADR-0035, regla 2).
func TestEveryRoleStarsWhatItReads(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(&testOperator)

			rec := patchStarred(mountDesk(role, d), target.ConversationID.String(), `{"starred":true}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("PATCH as %s: got %d, want 200 - body %s", role, rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), `"starred":true`) || !d.aggregate.starred[target.ConversationID] {
				t.Fatalf("want the star written and answered - body %s", rec.Body)
			}
		})
	}
}

// TestEachStarRefusalHasItsOwnCode is what the frontend switches on when the star does not move. False is a value and not an
// absence: clearing the star must never be read as a body without one.
func TestEachStarRefusalHasItsOwnCode(t *testing.T) {
	onePooled := func(d *desk) string { return d.open(nil).ConversationID.String() }

	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		shape    func(*desk) string
		body     string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", enums.OperatorRoleAdmin, func(*desk) string { return "no-es-un-id" }, `{"starred":true}`, http.StatusBadRequest, "invalid_conversation_id"},
		{"the body has no starred", enums.OperatorRoleAdmin, onePooled, `{}`, http.StatusBadRequest, "invalid_body"},
		{"starred is not a bool", enums.OperatorRoleAdmin, onePooled, `{"starred":"si"}`, http.StatusBadRequest, "invalid_body"},
		{"the conversation does not exist", enums.OperatorRoleAdmin, func(*desk) string { return id.New().String() }, `{"starred":true}`, http.StatusNotFound, "conversation_not_found"},
		{"another's conversation does not exist for an operator", enums.OperatorRoleOperator, func(d *desk) string {
			return d.open(&otherOperator).ConversationID.String()
		}, `{"starred":true}`, http.StatusNotFound, "conversation_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			conversationID := tc.shape(d)

			rec := patchStarred(mountDesk(tc.role, d), conversationID, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
			if len(d.aggregate.starred) != 0 {
				t.Fatal("a refused star was still written")
			}
		})
	}

	t.Run("false clears the star and is not read as missing", func(t *testing.T) {
		d := newDesk(&stubStore{})
		target := d.open(nil)
		target.Starred = true
		d.store.targets[target.ConversationID] = target

		rec := patchStarred(mountDesk(enums.OperatorRoleAdmin, d), target.ConversationID.String(), `{"starred":false}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"starred":false`) {
			t.Fatalf("got %d - body %s, want 200 with the star cleared", rec.Code, rec.Body)
		}
		if got, written := d.aggregate.starred[target.ConversationID]; !written || got {
			t.Fatalf("written = %v with %v, want false written", written, got)
		}
	})
}

// TestTheStarIsVisibleWithoutOpeningTheConversation is F25 read through the listing: filterable in the inbox starts by being on its row.
func TestTheStarIsVisibleWithoutOpeningTheConversation(t *testing.T) {
	store := &stubStore{rows: []Summary{
		{ID: id.New(), Starred: true, LastActivityAt: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)},
		{ID: id.New(), LastActivityAt: time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)},
	}}

	rec := get(mountDesk(enums.OperatorRoleAdmin, newDesk(store)), url.Values{})
	var page struct {
		Items []struct {
			Starred *bool `json:"starred"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
		t.Fatalf("the inbox is not a page of two: %v - body %s", err, rec.Body)
	}
	for i, want := range []bool{true, false} {
		if page.Items[i].Starred == nil || *page.Items[i].Starred != want {
			t.Fatalf("row %d starred = %v, want %v present - body %s", i, page.Items[i].Starred, want, rec.Body)
		}
	}
}

// TestTheStarHandlerRefusesWithoutAnActor guards the wiring the same way the other handlers do.
func TestTheStarHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodPatch, "/conversations/x/starred", strings.NewReader(`{"starred":true}`))

	NewController(newDesk(&stubStore{}).service, nil).setStarred(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func postRead(engine http.Handler, conversationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/read", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestEveryRoleClearsTheUnreadOfThePool walks the route with the three roles, because a wrong Roles list compiles (ADR-0052).
func TestEveryRoleClearsTheUnreadOfThePool(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			target.UnreadCount = 2
			d.store.targets[target.ConversationID] = target

			if rec := postRead(mountDesk(role, d), target.ConversationID.String()); rec.Code != http.StatusNoContent {
				t.Fatalf("POST as %s: got %d, want 204 - body %s", role, rec.Code, rec.Body)
			}
			if len(d.aggregate.read) != 1 {
				t.Fatal("the unread was not cleared")
			}
		})
	}
}

// TestEachReadRefusalHasItsOwnCode is what the frontend switches on: a supervisor learns the conversation is taken, an operator
// that it does not exist (ADR-0035, reglas 3 y 6).
func TestEachReadRefusalHasItsOwnCode(t *testing.T) {
	someoneElse := id.New()
	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		shape    func(*desk) string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", enums.OperatorRoleAdmin, func(*desk) string { return "no-es-un-id" }, http.StatusBadRequest, "invalid_conversation_id"},
		{"the conversation does not exist", enums.OperatorRoleAdmin, func(*desk) string { return id.New().String() }, http.StatusNotFound, "conversation_not_found"},
		{"a supervisor on a vendor's conversation", enums.OperatorRoleSupervisor, func(d *desk) string {
			return d.open(&someoneElse).ConversationID.String()
		}, http.StatusConflict, "conversation_taken"},
		{"an operator on another's conversation", enums.OperatorRoleOperator, func(d *desk) string {
			return d.open(&someoneElse).ConversationID.String()
		}, http.StatusNotFound, "conversation_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			rec := postRead(mountDesk(tc.role, d), tc.shape(d))
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheUnreadCountIsOnTheRow: the badge and the filter of F23 read it without opening the conversation (ADR-0052).
func TestTheUnreadCountIsOnTheRow(t *testing.T) {
	store := &stubStore{rows: []Summary{{ID: id.New(), UnreadCount: 4, LastActivityAt: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}}}
	rec := get(mountDesk(enums.OperatorRoleAdmin, newDesk(store)), url.Values{})
	if !strings.Contains(rec.Body.String(), `"unread_count":4`) {
		t.Fatalf("the row does not carry the unread count - body %s", rec.Body)
	}
}

// TestTheReadHandlerRefusesWithoutAnActor guards the wiring the same way the other handlers do.
func TestTheReadHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodPost, "/conversations/x/read", nil)

	NewController(newDesk(&stubStore{}).service, nil).markRead(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func postResolve(engine http.Handler, conversationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/resolve", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestEveryRoleClosesWhatItHolds walks the route with the three roles, because a wrong Roles list compiles (ADR-0054).
func TestEveryRoleClosesWhatItHolds(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(&testOperator)

			if rec := postResolve(mountDesk(role, d), target.ConversationID.String()); rec.Code != http.StatusNoContent {
				t.Fatalf("POST as %s: got %d, want 204 - body %s", role, rec.Code, rec.Body)
			}
			if len(d.aggregate.resolvedBy) != 1 {
				t.Fatal("the conversation was not closed")
			}
		})
	}
}

// TestEachCloseRefusalHasItsOwnCode is what the frontend switches on: the pool is taken first, a closed one says so (ADR-0054).
func TestEachCloseRefusalHasItsOwnCode(t *testing.T) {
	someoneElse := id.New()
	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		shape    func(*desk) string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", enums.OperatorRoleAdmin, func(*desk) string { return "no-es-un-id" }, http.StatusBadRequest, "invalid_conversation_id"},
		{"the conversation does not exist", enums.OperatorRoleAdmin, func(*desk) string { return id.New().String() }, http.StatusNotFound, "conversation_not_found"},
		{"an operator on another's conversation", enums.OperatorRoleOperator, func(d *desk) string {
			return d.open(&someoneElse).ConversationID.String()
		}, http.StatusNotFound, "conversation_not_found"},
		{"an operator on one of the pool", enums.OperatorRoleOperator, func(d *desk) string {
			return d.open(nil).ConversationID.String()
		}, http.StatusConflict, "conversation_not_assigned"},
		{"a conversation already closed", enums.OperatorRoleSupervisor, func(d *desk) string {
			target := d.open(&someoneElse)
			target.Status = enums.ConversationStatusResolved
			d.store.targets[target.ConversationID] = target
			return target.ConversationID.String()
		}, http.StatusConflict, "conversation_resolved"},
		{"the bot is still answering", enums.OperatorRoleAdmin, func(d *desk) string {
			d.aggregate.resolveErr = conversations.ErrTurnInProgress
			return d.open(&someoneElse).ConversationID.String()
		}, http.StatusConflict, "turn_in_progress"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			rec := postResolve(mountDesk(tc.role, d), tc.shape(d))
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheResolveHandlerRefusesWithoutAnActor guards the wiring the same way the other handlers do.
func TestTheResolveHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodPost, "/conversations/x/resolve", nil)

	NewController(newDesk(&stubStore{}).service, nil).resolve(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func putScore(engine http.Handler, conversationID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/conversations/"+conversationID+"/score", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// closedBy opens a resolved conversation held by holder, the only kind F13 scores (ADR-0055).
func closedBy(d *desk, holder *uuid.UUID) string {
	target := d.open(holder)
	target.Status = enums.ConversationStatusResolved
	d.store.targets[target.ConversationID] = target
	return target.ConversationID.String()
}

// TestEveryRoleScoresWhatItHeld walks the route with the three roles, because a wrong Roles list compiles (ADR-0055).
func TestEveryRoleScoresWhatItHeld(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})

			rec := putScore(mountDesk(role, d), closedBy(d, &testOperator), `{"score":4}`)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"score":4`) {
				t.Fatalf("PUT as %s: got %d, want 200 with the score - body %s", role, rec.Code, rec.Body)
			}
			if len(d.aggregate.scores) != 1 {
				t.Fatal("the score was not written")
			}
		})
	}
}

// TestEachScoreRefusalHasItsOwnCode is what the frontend switches on when the score does not land (ADR-0055).
func TestEachScoreRefusalHasItsOwnCode(t *testing.T) {
	mine := func(d *desk) string { return closedBy(d, &testOperator) }

	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		shape    func(*desk) string
		body     string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", enums.OperatorRoleOperator, func(*desk) string { return "no-es-un-id" }, `{"score":4}`, http.StatusBadRequest, "invalid_conversation_id"},
		{"the body has no score", enums.OperatorRoleOperator, mine, `{}`, http.StatusBadRequest, "invalid_body"},
		{"the score is not a whole number", enums.OperatorRoleOperator, mine, `{"score":4.5}`, http.StatusBadRequest, "invalid_body"},
		{"the score is off the scale", enums.OperatorRoleOperator, mine, `{"score":6}`, http.StatusBadRequest, "invalid_score"},
		{"the conversation does not exist", enums.OperatorRoleAdmin, func(*desk) string { return id.New().String() }, `{"score":4}`, http.StatusNotFound, "conversation_not_found"},
		{"another's conversation does not exist for an operator", enums.OperatorRoleOperator, func(d *desk) string {
			return closedBy(d, &otherOperator)
		}, `{"score":4}`, http.StatusNotFound, "conversation_not_found"},
		{"an open conversation", enums.OperatorRoleOperator, func(d *desk) string {
			return d.open(&testOperator).ConversationID.String()
		}, `{"score":4}`, http.StatusConflict, "conversation_not_resolved"},
		{"a supervisor on a vendor's", enums.OperatorRoleSupervisor, func(d *desk) string {
			return closedBy(d, &otherOperator)
		}, `{"score":4}`, http.StatusConflict, "not_the_vendor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			rec := putScore(mountDesk(tc.role, d), tc.shape(d), tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
			if len(d.aggregate.scores) != 0 {
				t.Fatal("a refused score was still written")
			}
		})
	}
}

// TestTheScoreIsOnTheRow is F13 read through the listing: null until the vendor scores, so the frontend tells unscored from a score.
func TestTheScoreIsOnTheRow(t *testing.T) {
	four := 4
	store := &stubStore{rows: []Summary{
		{ID: id.New(), QualityScore: &four, LastActivityAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{ID: id.New(), LastActivityAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
	}}

	rec := get(mountDesk(enums.OperatorRoleAdmin, newDesk(store)), url.Values{})
	if !strings.Contains(rec.Body.String(), `"quality_score":4`) || !strings.Contains(rec.Body.String(), `"quality_score":null`) {
		t.Fatalf("want one row scored 4 and one null - body %s", rec.Body)
	}
}
