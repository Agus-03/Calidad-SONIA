package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount   = id.New()
	testOperator  = id.New()
	otherOperator = id.New()
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

func get(engine http.Handler, query url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

type decodedPage struct {
	Items []struct {
		ID          string               `json:"id"`
		Operator    *struct{ ID string } `json:"operator"`
		LastMessage *struct {
			Direction string  `json:"direction"`
			Status    *string `json:"status"`
			ErrorCode *string `json:"error_code"`
		} `json:"last_message"`
		FailedCount  int     `json:"failed_count"`
		LastFailedAt *string `json:"last_failed_at"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) decodedPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /conversations: got %d, want 200 - body %s", rec.Code, rec.Body)
	}
	var page decodedPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the page is not json: %v - body %s", err, rec.Body)
	}
	return page
}

func ids(page decodedPage) []string {
	out := []string{}
	for _, item := range page.Items {
		out = append(out, item.ID)
	}
	return out
}

// team is one conversation of each kind the reach tells apart, newest first: another operator's, the caller's and the pool's.
func team() (store *stubStore, theirs, mine, pooled Summary) {
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	theirs = Summary{ID: id.New(), LastActivityAt: at, Operator: &Operator{ID: otherOperator, Name: "Beto", Lastname: "Ajeno"}}
	mine = Summary{ID: id.New(), LastActivityAt: at.Add(-time.Minute), Operator: &Operator{ID: testOperator, Name: "Ana", Lastname: "Propia"}}
	pooled = Summary{ID: id.New(), LastActivityAt: at.Add(-2 * time.Minute)}
	return &stubStore{rows: []Summary{pooled, theirs, mine}}, theirs, mine, pooled
}

// TestEachRoleSeesTheInboxAnexoBGivesIt is the reach written both ways: an operator that saw another's conversation would break
// Anexo B, and one that did not see the pool could never answer first, which is the only way ADR-0004 assigns anything.
func TestEachRoleSeesTheInboxAnexoBGivesIt(t *testing.T) {
	store, theirs, mine, pooled := team()

	for _, tc := range []struct {
		role enums.OperatorRole
		want []string
	}{
		{enums.OperatorRoleOperator, []string{mine.ID.String(), pooled.ID.String()}},
		{enums.OperatorRoleSupervisor, []string{theirs.ID.String(), mine.ID.String(), pooled.ID.String()}},
		{enums.OperatorRoleAdmin, []string{theirs.ID.String(), mine.ID.String(), pooled.ID.String()}},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			got := ids(decode(t, get(mount(tc.role, store), nil)))
			if len(got) != len(tc.want) {
				t.Fatalf("the inbox holds %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("the inbox holds %v, want %v in that order", got, tc.want)
				}
			}
		})
	}
}

// TestWalkingTheCursorShowsEveryConversationOnce is what the cursor was chosen for: page after page, nothing repeated, nothing skipped,
// and the last page says so with a null instead of a token that leads nowhere.
func TestWalkingTheCursorShowsEveryConversationOnce(t *testing.T) {
	store, theirs, mine, pooled := team()
	engine := mount(enums.OperatorRoleAdmin, store)

	seen := []string{}
	query := url.Values{"limit": {"1"}}
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatalf("still walking after %d pages: the cursor never ends", pages)
		}
		page := decode(t, get(engine, query))
		seen = append(seen, ids(page)...)
		if page.NextCursor == nil {
			break
		}
		query.Set("cursor", *page.NextCursor)
	}

	want := []string{theirs.ID.String(), mine.ID.String(), pooled.ID.String()}
	if len(seen) != len(want) {
		t.Fatalf("walked %v, want %v", seen, want)
	}
	for i := range seen {
		if seen[i] != want[i] {
			t.Fatalf("walked %v, want %v", seen, want)
		}
	}
}

// TestAFailedSendIsVisibleWithoutOpeningTheConversation is ADR-0020's condition: at most once is only acceptable while the failure is
// in front of somebody. It asserts the case that would hide it, too: an inbound message arriving after the failed one.
func TestAFailedSendIsVisibleWithoutOpeningTheConversation(t *testing.T) {
	failed := enums.MessageStatusFailed
	code := "131047"
	failedAt := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)

	lastFailed := Summary{
		ID: id.New(), LastActivityAt: failedAt.Add(time.Hour),
		LastMessage: &LastMessage{ID: id.New(), Direction: enums.MessageDirectionOutbound, SenderType: enums.SenderTypeAiAgent,
			ContentText: "buenas", ContentType: enums.MessageContentTypeText, Status: &failed, ErrorCode: &code, SentAt: failedAt},
		FailedCount: 1, LastFailedAt: &failedAt,
	}
	buried := Summary{
		ID: id.New(), LastActivityAt: failedAt,
		LastMessage: &LastMessage{ID: id.New(), Direction: enums.MessageDirectionInbound, SenderType: enums.SenderTypeContact,
			ContentText: "hola?", ContentType: enums.MessageContentTypeText, SentAt: failedAt},
		FailedCount: 1, LastFailedAt: &failedAt,
	}

	page := decode(t, get(mount(enums.OperatorRoleOperator, &stubStore{rows: []Summary{lastFailed, buried}}), nil))
	if len(page.Items) != 2 {
		t.Fatalf("the inbox holds %d conversations, want 2", len(page.Items))
	}

	first := page.Items[0]
	if first.LastMessage == nil || first.LastMessage.Status == nil || *first.LastMessage.Status != "failed" {
		t.Fatalf("the last message does not say it failed: %+v", first.LastMessage)
	}
	if first.LastMessage.ErrorCode == nil || *first.LastMessage.ErrorCode != code {
		t.Fatalf("the failure travels without its error: %+v", first.LastMessage)
	}

	second := page.Items[1]
	if second.LastMessage == nil || second.LastMessage.Direction != "inbound" {
		t.Fatalf("want the inbound message as the last one: %+v", second.LastMessage)
	}
	if second.FailedCount != 1 || second.LastFailedAt == nil {
		t.Fatalf("an inbound message buried the failure: failed_count %d last_failed_at %v", second.FailedCount, second.LastFailedAt)
	}
}

// TestThePoolAndAnEmptyConversationAreNullAndNotZero keeps "nobody" from reading as somebody: an operator with an empty id, or a last
// message with no text, would be drawn by the frontend as if they existed.
func TestThePoolAndAnEmptyConversationAreNullAndNotZero(t *testing.T) {
	store := &stubStore{rows: []Summary{{ID: id.New(), LastActivityAt: time.Now()}}}

	rec := get(mount(enums.OperatorRoleOperator, store), nil)
	var raw struct {
		Items      []map[string]json.RawMessage `json:"items"`
		NextCursor json.RawMessage              `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Items) != 1 {
		t.Fatalf("want one conversation, got %v - body %s", err, rec.Body)
	}
	for _, field := range []string{"operator", "last_message", "ai_agent_id", "last_failed_at"} {
		if string(raw.Items[0][field]) != "null" {
			t.Fatalf("%s is %s, want null", field, raw.Items[0][field])
		}
	}
	if string(raw.NextCursor) != "null" {
		t.Fatalf("next_cursor is %s on the only page, want null", raw.NextCursor)
	}
}

// TestAQueryTheListingCannotHonourIsRefused names each malformed input: guessing a cursor would answer the first page again and the
// frontend would draw it as the next one.
func TestAQueryTheListingCannotHonourIsRefused(t *testing.T) {
	encoded := func(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }

	for _, tc := range []struct {
		name     string
		query    url.Values
		wantCode string
	}{
		{"a cursor that is not base64", url.Values{"cursor": {"no es base64!"}}, "invalid_cursor"},
		{"a cursor with no separator", url.Values{"cursor": {encoded("sin-separador")}}, "invalid_cursor"},
		{"a cursor whose instant is not one", url.Values{"cursor": {encoded("ayer|" + id.New().String())}}, "invalid_cursor"},
		{"a cursor whose id is not a uuid", url.Values{"cursor": {encoded(time.Now().Format(time.RFC3339Nano) + "|nada")}}, "invalid_cursor"},
		{"a limit of zero", url.Values{"limit": {"0"}}, "invalid_limit"},
		{"a negative limit", url.Values{"limit": {"-3"}}, "invalid_limit"},
		{"a limit that is not a number", url.Values{"limit": {"veinte"}}, "invalid_limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, _, _, _ := team()
			rec := get(mount(enums.OperatorRoleAdmin, store), tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tc.wantCode {
				t.Fatalf("code %q, want %q - body %s", body.Error.Code, tc.wantCode, rec.Body)
			}
		})
	}
}

// TestALimitAboveTheCeilingIsClampedNotRefused matches per_page in contacts/: asking for too many is not worth failing the page over.
func TestALimitAboveTheCeilingIsClampedNotRefused(t *testing.T) {
	store, _, _, _ := team()

	decode(t, get(mount(enums.OperatorRoleAdmin, store), url.Values{"limit": {"100000"}}))
	if store.lastLimit != maxLimit+1 {
		t.Fatalf("the store was asked for %d rows, want the ceiling plus the one past it, %d", store.lastLimit, maxLimit+1)
	}
}

// TestAStoreFailureIsA500AndNotAnEmptyInbox is the difference between "nothing is waiting" and "we could not look".
func TestAStoreFailureIsA500AndNotAnEmptyInbox(t *testing.T) {
	rec := get(mount(enums.OperatorRoleAdmin, &stubStore{err: errors.New("connection refused")}), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500 - body %s", rec.Code, rec.Body)
	}
}

// TestAHandlerMountedWithoutAnActorRefuses guards the wiring: an empty scope would narrow to the pool and look like a working inbox.
func TestAHandlerMountedWithoutAnActorRefuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodGet, "/conversations", nil)

	NewController(NewService(&stubStore{}, nil), nil).list(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}
