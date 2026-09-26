package inbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/turn"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

var (
	testAccount   = id.New()
	testOperator  = id.New()
	otherOperator = id.New()
)

// mount builds the real router, because what these cases assert is the roles declared in routes.go and those only exist there.
func mount(role enums.OperatorRole, store *stubStore) http.Handler {
	return mountDesk(role, newDesk(store))
}

func mountDesk(role enums.OperatorRole, d *desk) http.Handler {
	return soniahttp.NewRouter(soniahttp.RouterOptions{
		Controllers:   []soniahttp.Controller{NewController(d.service, nil)},
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
// Anexo B, and one that did not see the pool could never answer first, which is the only way ADR-0004 assigns anything (ADR-0035, regla 1).
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
// and the last page says so with a null instead of a token that leads nowhere (ADR-0036, reglas 3 y 6).
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
		LastMessage: &Message{ID: id.New(), Direction: enums.MessageDirectionOutbound, SenderType: enums.SenderTypeAiAgent,
			ContentText: "buenas", ContentType: enums.MessageContentTypeText, Status: &failed, ErrorCode: &code, SentAt: failedAt},
		FailedCount: 1, LastFailedAt: &failedAt,
	}
	buried := Summary{
		ID: id.New(), LastActivityAt: failedAt,
		LastMessage: &Message{ID: id.New(), Direction: enums.MessageDirectionInbound, SenderType: enums.SenderTypeContact,
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
	// And a conversation with no agent says its bot cannot answer, as false and never as a missing field (ADR-0038).
	if string(raw.Items[0]["ai_agent_available"]) != "false" {
		t.Fatalf("ai_agent_available is %s, want false", raw.Items[0]["ai_agent_available"])
	}
}

// TestAQueryTheListingCannotHonourIsRefused names each malformed input: guessing a cursor would answer the first page again and the
// frontend would draw it as the next one (ADR-0036, regla 5).
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
		// A flag is on only with true: "1" read as off would draw the whole inbox as the filtered one (ADR-0050).
		{"a flag that is not true", url.Values{"unread": {"1"}}, "invalid_filter"},
		{"false is not how a flag is turned off", url.Values{"starred": {"false"}}, "invalid_filter"},
		{"a status that is not one of the three", url.Values{"status": {"closed"}}, "invalid_status"},
		{"a label id that is not a uuid", url.Values{"label_id": {"ventas"}}, "invalid_label_id"},
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

// TestALimitAboveTheCeilingIsClampedNotRefused matches per_page in contacts/: asking for too many is not worth failing the page over
// (ADR-0036, regla 8).
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

	NewController(newDesk(&stubStore{}).service, nil).list(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func post(engine http.Handler, conversationID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", strings.NewReader(body))
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
		t.Fatalf("the error is not json: %v - body %s", err, rec.Body)
	}
	return body.Error.Code
}

// TestEveryRoleCanAnswerThePool walks the route with the three roles, because a wrong Roles list compiles: an operator refused here
// could never take the pool, and ADR-0004 would assign nothing.
func TestEveryRoleCanAnswerThePool(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)

			rec := post(mountDesk(role, d), target.ConversationID.String(), `{"text":"buenas, te ayudo"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST as %s: got %d, want 201 - body %s", role, rec.Code, rec.Body)
			}
			var body struct {
				ID         string  `json:"id"`
				SenderType string  `json:"sender_type"`
				Status     *string `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("the answer is not json: %v", err)
			}
			if body.SenderType != "operator" || body.Status == nil || *body.Status != "sent" {
				t.Fatalf("got sender %q and status %v, want operator and sent - body %s", body.SenderType, body.Status, rec.Body)
			}
		})
	}
}

// TestAFailedSendAnswers201WithTheFailure is the choice of the handler: the row exists, and its status is what the operator reads.
func TestAFailedSendAnswers201WithTheFailure(t *testing.T) {
	d := newDesk(&stubStore{})
	d.sender.err = &httpclient.StatusError{Status: 400, Body: `{"error":{"message":"(#131030) Recipient not in allowed list","code":131030}}`}
	target := d.open(nil)

	rec := post(mountDesk(enums.OperatorRoleOperator, d), target.ConversationID.String(), `{"text":"hola"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201 - body %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"status":"failed"`) || !strings.Contains(rec.Body.String(), `"error_code":"131030"`) {
		t.Fatalf("the failure did not travel with the row - body %s", rec.Body)
	}
	// Meta's code and never the body Meta answered with: that belongs in the log, not in front of the operator.
	if strings.Contains(rec.Body.String(), "Recipient not in allowed list") {
		t.Fatalf("the cause leaked into the answer - body %s", rec.Body)
	}
}

// TestEachRefusalHasItsOwnCode is what the frontend switches on: a code per reason, and never a 500 for something the caller can fix
// (ADR-0035, reglas 3, 6, 8 y 9).
func TestEachRefusalHasItsOwnCode(t *testing.T) {
	other := id.New()

	for _, tc := range []struct {
		name     string
		role     enums.OperatorRole
		shape    func(*desk) string
		body     string
		wantCode int
		want     string
	}{
		{"the id is not a uuid", enums.OperatorRoleOperator,
			func(*desk) string { return "no-es-un-id" }, `{"text":"hola"}`, http.StatusBadRequest, "invalid_conversation_id"},
		{"the body is not json", enums.OperatorRoleOperator,
			func(d *desk) string { return d.open(nil).ConversationID.String() }, `hola`, http.StatusBadRequest, "invalid_body"},
		// An absent text and a blank one are one refusal with one code, so the frontend switches on one word for both.
		{"the body has no text", enums.OperatorRoleOperator,
			func(d *desk) string { return d.open(nil).ConversationID.String() }, `{}`, http.StatusBadRequest, "invalid_text"},
		{"the text is only blanks", enums.OperatorRoleOperator,
			func(d *desk) string { return d.open(nil).ConversationID.String() }, `{"text":"   "}`, http.StatusBadRequest, "invalid_text"},
		{"the conversation does not exist", enums.OperatorRoleOperator,
			func(*desk) string { return id.New().String() }, `{"text":"hola"}`, http.StatusNotFound, "conversation_not_found"},
		{"another operator's conversation, asked by an operator", enums.OperatorRoleOperator,
			func(d *desk) string { return d.open(&other).ConversationID.String() }, `{"text":"hola"}`, http.StatusNotFound, "conversation_not_found"},
		{"another operator's conversation, asked by a supervisor", enums.OperatorRoleSupervisor,
			func(d *desk) string { return d.open(&other).ConversationID.String() }, `{"text":"hola"}`, http.StatusConflict, "conversation_taken"},
		{"the pool race lost", enums.OperatorRoleOperator,
			func(d *desk) string {
				d.aggregate.claimErr = conversations.ErrTaken
				return d.open(nil).ConversationID.String()
			}, `{"text":"hola"}`, http.StatusConflict, "conversation_taken"},
		{"the conversation is resolved", enums.OperatorRoleOperator,
			func(d *desk) string {
				target := d.open(nil)
				target.Status = enums.ConversationStatusResolved
				d.store.targets[target.ConversationID] = target
				return target.ConversationID.String()
			}, `{"text":"hola"}`, http.StatusConflict, "conversation_resolved"},
		{"the window closed", enums.OperatorRoleOperator,
			func(d *desk) string {
				target := d.open(nil)
				target.WindowExpiresAt = time.Now().Add(-time.Minute)
				d.store.targets[target.ConversationID] = target
				return target.ConversationID.String()
			}, `{"text":"hola"}`, http.StatusConflict, "window_expired"},
		{"the number cannot send", enums.OperatorRoleOperator,
			func(d *desk) string {
				d.sender.readyErr = whatsapp.ErrInboxNotConfigured
				return d.open(nil).ConversationID.String()
			}, `{"text":"hola"}`, http.StatusConflict, "channel_unavailable"},
		{"the database did not answer", enums.OperatorRoleOperator,
			func(d *desk) string {
				d.store.err = errors.New("connection refused")
				return id.New().String()
			}, `{"text":"hola"}`, http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			conversationID := tc.shape(d)

			rec := post(mountDesk(tc.role, d), conversationID, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
			if d.sender.calls != 0 {
				t.Fatalf("a refused answer reached the customer")
			}
		})
	}
}

// TestTheSendHandlerRefusesWithoutAnActor guards the wiring the same way the listing's does.
func TestTheSendHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodPost, "/conversations/x/messages", strings.NewReader(`{"text":"hola"}`))

	NewController(newDesk(&stubStore{}).service, nil).send(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

func getThread(engine http.Handler, conversationID string, query url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestTheThreadShowsEachFailureWhereItHappened is ADR-0020 completed: the failed answer is in its place in the thread with its code,
// and who wrote each message is said, which is what the operator reads before deciding to send it again.
func TestTheThreadShowsEachFailureWhereItHappened(t *testing.T) {
	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			d := newDesk(&stubStore{})
			target := d.open(nil)
			written := d.write(target.ConversationID, 1)

			failed, code := enums.MessageStatusFailed, "131047"
			answer := ThreadMessage{
				Message: Message{
					ID: id.New(), Direction: enums.MessageDirectionOutbound, SenderType: enums.SenderTypeOperator,
					ContentText: "te respondo", ContentType: enums.MessageContentTypeText, Status: &failed, ErrorCode: &code,
					SentAt: written[0].SentAt.Add(time.Minute),
				},
				SenderOperator: &Operator{ID: testOperator, Name: "Ana", Lastname: "Propia"},
				CreatedAt:      written[0].CreatedAt.Add(time.Minute),
			}
			d.store.threads[target.ConversationID] = append(d.store.threads[target.ConversationID], answer)

			rec := getThread(mountDesk(role, d), target.ConversationID.String(), nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET the thread as %s: got %d, want 200 - body %s", role, rec.Code, rec.Body)
			}
			var page struct {
				Items []struct {
					ID             string  `json:"id"`
					Status         *string `json:"status"`
					ErrorCode      *string `json:"error_code"`
					SenderType     string  `json:"sender_type"`
					SenderOperator *struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"sender_operator"`
				} `json:"items"`
				NextCursor *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 2 {
				t.Fatalf("want two messages, got %v - body %s", err, rec.Body)
			}

			first := page.Items[0]
			if first.ID != answer.ID.String() || first.Status == nil || *first.Status != "failed" || first.ErrorCode == nil || *first.ErrorCode != code {
				t.Fatalf("the newest message should be the failed answer with its cause - body %s", rec.Body)
			}
			if first.SenderOperator == nil || first.SenderOperator.Name != "Ana" {
				t.Fatalf("the operator's answer does not say who wrote it - body %s", rec.Body)
			}
			if page.Items[1].SenderType != "contact" || page.Items[1].SenderOperator != nil {
				t.Fatalf("the customer's message carries an operator - body %s", rec.Body)
			}
			if page.NextCursor != nil {
				t.Fatalf("a thread that fits in one page handed out a cursor")
			}
		})
	}
}

// TestWalkingTheThreadShowsEveryMessageOnce is the cursor of the listing on a thread, end to end through the router.
func TestWalkingTheThreadShowsEveryMessageOnce(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(nil)
	want := d.write(target.ConversationID, 3)
	engine := mountDesk(enums.OperatorRoleOperator, d)

	seen := []string{}
	query := url.Values{"limit": {"1"}}
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatalf("still walking after %d pages", pages)
		}
		rec := getThread(engine, target.ConversationID.String(), query)
		var page struct {
			Items      []struct{ ID string } `json:"items"`
			NextCursor *string               `json:"next_cursor"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("the page is not json: %v - body %s", err, rec.Body)
		}
		for _, m := range page.Items {
			seen = append(seen, m.ID)
		}
		if page.NextCursor == nil {
			break
		}
		query.Set("cursor", *page.NextCursor)
	}

	if len(seen) != len(want) {
		t.Fatalf("walked %v, want %d messages", seen, len(want))
	}
	for i := range want {
		if seen[i] != want[i].ID.String() {
			t.Fatalf("walked %v, want newest first", seen)
		}
	}
}

// TestEachThreadRefusalHasItsOwnCode is what the frontend switches on when a thread cannot be drawn.
func TestEachThreadRefusalHasItsOwnCode(t *testing.T) {
	other := id.New()

	for _, tc := range []struct {
		name     string
		shape    func(*desk) string
		query    url.Values
		wantCode int
		want     string
	}{
		{"the id is not a uuid", func(*desk) string { return "no-es-un-id" }, nil, http.StatusBadRequest, "invalid_conversation_id"},
		{"a cursor it never handed out", func(d *desk) string { return d.open(nil).ConversationID.String() },
			url.Values{"cursor": {"no es base64!"}}, http.StatusBadRequest, "invalid_cursor"},
		{"a limit of zero", func(d *desk) string { return d.open(nil).ConversationID.String() },
			url.Values{"limit": {"0"}}, http.StatusBadRequest, "invalid_limit"},
		{"another operator's conversation", func(d *desk) string { return d.open(&other).ConversationID.String() },
			nil, http.StatusNotFound, "conversation_not_found"},
		{"the database did not answer", func(d *desk) string {
			d.store.threadErr = errors.New("connection refused")
			return d.open(nil).ConversationID.String()
		}, nil, http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			conversationID := tc.shape(d)

			rec := getThread(mountDesk(enums.OperatorRoleOperator, d), conversationID, tc.query)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.wantCode, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.want {
				t.Fatalf("code %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTheThreadHandlerRefusesWithoutAnActor guards the wiring the same way the other handlers do.
func TestTheThreadHandlerRefusesWithoutAnActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	gc, _ := gin.CreateTestContext(rec)
	gc.Request = httptest.NewRequest(http.MethodGet, "/conversations/x/messages", nil)

	NewController(newDesk(&stubStore{}).service, nil).thread(gc)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 - body %s", rec.Code, rec.Body)
	}
}

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

func getSuggestion(engine http.Handler, conversationID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+conversationID+"/suggestion", nil)
	req.Header.Set("Authorization", "Bearer cualquier-cosa")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// ADR-0046, section 2.5: the draft waiting is read with its id, which is what the send names; no draft is a 204 and not an error.
func TestTheSuggestionHandlerAnswersTheDraftOrNothing(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	draft := id.New()
	d.store.suggestions = map[uuid.UUID]Suggestion{target.ConversationID: {GenerationID: draft, Text: "Sí, tenemos dos."}}
	engine := mountDesk(enums.OperatorRoleOperator, d)

	rec := getSuggestion(engine, target.ConversationID.String())
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), draft.String()) {
		t.Fatalf("GET suggestion: got %d - %s, want 200 with the draft", rec.Code, rec.Body)
	}

	empty := d.open(&testOperator)
	if rec := getSuggestion(engine, empty.ConversationID.String()); rec.Code != http.StatusNoContent {
		t.Fatalf("GET suggestion with no draft: got %d, want 204", rec.Code)
	}
	if rec := getSuggestion(engine, "no-es-un-id"); errorCode(t, rec) != "invalid_conversation_id" {
		t.Fatalf("GET suggestion with a bad id: got %d - %s", rec.Code, rec.Body)
	}
}

// The send names the draft it copied by id: a bad one is refused before anything is written, and one that is not pending is a 409.
func TestASendThatNamesASuggestionHasItsOwnRefusals(t *testing.T) {
	d := newDesk(&stubStore{})
	target := d.open(&testOperator)
	engine := mountDesk(enums.OperatorRoleOperator, d)

	rec := post(engine, target.ConversationID.String(), `{"text":"hola","generation_id":"no-es-un-id"}`)
	if got := errorCode(t, rec); got != "invalid_generation_id" {
		t.Fatalf("a malformed generation_id: got %q, want invalid_generation_id", got)
	}

	d.reviews.err = turn.ErrGenerationNotFound
	rec = post(engine, target.ConversationID.String(), `{"text":"hola","generation_id":"`+id.New().String()+`"}`)
	if got := errorCode(t, rec); got != "suggestion_not_pending" {
		t.Fatalf("a draft that is not pending: got %q, want suggestion_not_pending", got)
	}
	if d.sender.calls != 0 {
		t.Fatal("the answer left although its review was refused")
	}
}

// filtered is one conversation for each filter of F23 and one that matches none, newest first, so each case can name what it expects.
type filtered struct {
	store                                        *stubStore
	unread, assigned, starred, waiting, resolved Summary
	labelled, reminded, bare                     Summary
	label                                        uuid.UUID
}

func filteredInbox() filtered {
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	row := func(minutes int) Summary {
		return Summary{ID: id.New(), Status: enums.ConversationStatusActive, LastActivityAt: at.Add(-time.Duration(minutes) * time.Minute)}
	}
	f := filtered{
		unread: row(0), assigned: row(1), starred: row(2), waiting: row(3),
		resolved: row(4), labelled: row(5), reminded: row(6), bare: row(7),
		label: id.New(),
	}
	f.assigned.Operator = &Operator{ID: testOperator, Name: "Ana", Lastname: "Propia"}
	f.starred.Starred = true
	f.waiting.Status = enums.ConversationStatusWaiting
	f.resolved.Status = enums.ConversationStatusResolved
	f.store = &stubStore{
		rows:      []Summary{f.bare, f.reminded, f.labelled, f.resolved, f.waiting, f.starred, f.assigned, f.unread},
		unread:    map[uuid.UUID]bool{f.unread.ID: true},
		labels:    map[uuid.UUID][]uuid.UUID{f.labelled.ID: {id.New(), f.label}},
		reminders: map[uuid.UUID]bool{f.reminded.ID: true},
	}
	return f
}

// TestEachFilterOfF23NarrowsTheInboxToItsOwn walks the seven filters one by one through the real router: each one answers the
// conversation it names and nothing else, so no filter is silently ignored or read as another.
func TestEachFilterOfF23NarrowsTheInboxToItsOwn(t *testing.T) {
	f := filteredInbox()

	for _, tc := range []struct {
		name  string
		query url.Values
		want  Summary
	}{
		{"no leídas", url.Values{"unread": {"true"}}, f.unread},
		{"asignadas", url.Values{"assigned": {"true"}}, f.assigned},
		{"destacadas (F25)", url.Values{"starred": {"true"}}, f.starred},
		{"en espera", url.Values{"status": {"waiting"}}, f.waiting},
		{"resueltas", url.Values{"status": {"resolved"}}, f.resolved},
		{"por etiqueta", url.Values{"label_id": {f.label.String()}}, f.labelled},
		{"con recordatorio", url.Values{"reminder": {"true"}}, f.reminded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(decode(t, get(mount(enums.OperatorRoleAdmin, f.store), tc.query)))
			if len(got) != 1 || got[0] != tc.want.ID.String() {
				t.Fatalf("the filtered inbox holds %v, want only %v", got, tc.want.ID)
			}
		})
	}

	if got := ids(decode(t, get(mount(enums.OperatorRoleAdmin, f.store), nil))); len(got) != 8 {
		t.Fatalf("with no filter the inbox holds %d conversations, want all 8", len(got))
	}
}

// TestTheFiltersCombineAndNeverWidenTheReach is "combinables" read as AND, applied inside what the role already sees: an operator
// asking for the assigned ones gets its own and never another's (ADR-0035, regla 1; ADR-0050).
func TestTheFiltersCombineAndNeverWidenTheReach(t *testing.T) {
	at := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	mineStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at, Operator: &Operator{ID: testOperator}}
	mineWaiting := Summary{ID: id.New(), Status: enums.ConversationStatusWaiting, Starred: true, LastActivityAt: at.Add(-time.Minute), Operator: &Operator{ID: testOperator}}
	theirsStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at.Add(-2 * time.Minute), Operator: &Operator{ID: otherOperator}}
	pooledStarred := Summary{ID: id.New(), Starred: true, LastActivityAt: at.Add(-3 * time.Minute)}
	store := &stubStore{rows: []Summary{mineStarred, mineWaiting, theirsStarred, pooledStarred}}

	for _, tc := range []struct {
		name  string
		role  enums.OperatorRole
		query url.Values
		want  []Summary
	}{
		{"an operator's assigned ones are its own", enums.OperatorRoleOperator, url.Values{"assigned": {"true"}}, []Summary{mineStarred, mineWaiting}},
		{"an operator's starred ones stay inside its reach", enums.OperatorRoleOperator, url.Values{"starred": {"true"}}, []Summary{mineStarred, mineWaiting, pooledStarred}},
		{"two filters are both required", enums.OperatorRoleOperator, url.Values{"starred": {"true"}, "status": {"waiting"}}, []Summary{mineWaiting}},
		{"a supervisor's assigned ones are the whole team's", enums.OperatorRoleSupervisor, url.Values{"assigned": {"true"}}, []Summary{mineStarred, mineWaiting, theirsStarred}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(decode(t, get(mount(tc.role, store), tc.query)))
			if len(got) != len(tc.want) {
				t.Fatalf("the inbox holds %v, want %d conversations", got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i].ID.String() {
					t.Fatalf("the inbox holds %v, want %v in that order", got, tc.want[i].ID)
				}
			}
		})
	}
}

// TestTheCursorContinuesAFilteredInbox is the rule that closed ADR-0036's open question: no filter changes the order, so the same
// cursor walks a filtered listing, as long as the frontend keeps sending the same filter with it (ADR-0050).
func TestTheCursorContinuesAFilteredInbox(t *testing.T) {
	f := filteredInbox()
	f.store.rows[0].Starred, f.store.rows[2].Starred = true, true // the bare and the labelled one join the starred one
	engine := mount(enums.OperatorRoleAdmin, f.store)

	seen := []string{}
	query := url.Values{"limit": {"1"}, "starred": {"true"}}
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

	want := []string{f.starred.ID.String(), f.labelled.ID.String(), f.bare.ID.String()}
	if len(seen) != len(want) {
		t.Fatalf("walked %v, want %v", seen, want)
	}
	for i := range seen {
		if seen[i] != want[i] {
			t.Fatalf("walked %v, want %v", seen, want)
		}
	}
}
