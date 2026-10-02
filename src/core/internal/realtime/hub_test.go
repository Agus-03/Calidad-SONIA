package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/inbox"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

var (
	testAccount  = id.New()
	testOperator = id.New()
)

// stubRows is the one read of the hub, answered from memory: the account and the row of each conversation.
type stubRows struct {
	mu   sync.Mutex
	rows map[uuid.UUID]stubRow
	// The conversations whose read fails, one by one, so a case orders a failure among successes without timing it.
	failing map[uuid.UUID]bool
}

type stubRow struct {
	account uuid.UUID
	summary inbox.Summary
}

func (r *stubRows) SummaryOf(_ context.Context, conversationID uuid.UUID) (uuid.UUID, inbox.Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failing[conversationID] {
		return uuid.Nil, inbox.Summary{}, errors.New("connection refused")
	}
	row, ok := r.rows[conversationID]
	if !ok {
		return uuid.Nil, inbox.Summary{}, inbox.ErrNotFound
	}
	return row.account, row.summary, nil
}

func (r *stubRows) put(account uuid.UUID, holder *uuid.UUID) uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	conversationID := id.New()
	s := inbox.Summary{ID: conversationID, Status: enums.ConversationStatusActive, Mode: enums.AgentModeOff}
	if holder != nil {
		s.Operator = &inbox.Operator{ID: *holder}
	}
	r.rows[conversationID] = stubRow{account: account, summary: s}
	return conversationID
}

// line is one process with the hub behind its public gate, and the actors the lookup knows.
type line struct {
	hub    *Hub
	rows   *stubRows
	server *httptest.Server
	actors map[uuid.UUID]soniahttp.Actor
	dialed int
	mu     sync.Mutex
}

func newLine(t *testing.T) *line {
	t.Helper()
	l := &line{rows: &stubRows{rows: map[uuid.UUID]stubRow{}}, actors: map[uuid.UUID]soniahttp.Actor{}}
	l.hub = NewHub(HubOptions{Rows: l.rows})
	lookup := func(_ context.Context, operatorID uuid.UUID) (soniahttp.Actor, error) {
		l.mu.Lock()
		defer l.mu.Unlock()
		actor, ok := l.actors[operatorID]
		if !ok {
			return soniahttp.Actor{}, soniahttp.Unauthorized("session_no_longer_valid", "no such operator")
		}
		return actor, nil
	}
	l.server = httptest.NewServer(soniahttp.NewRouter(soniahttp.RouterOptions{
		PublicControllers: []soniahttp.Controller{NewStreamGate(l.hub, lookup, nil)},
	}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = l.hub.Close(ctx)
		l.server.Close()
	})
	return l
}

func (l *line) actor(account uuid.UUID, role enums.OperatorRole) uuid.UUID {
	l.mu.Lock()
	defer l.mu.Unlock()
	operatorID := id.New()
	l.actors[operatorID] = soniahttp.Actor{OperatorID: operatorID, AccountID: account, Role: role}
	return operatorID
}

// connect opens a socket the way the frontend will: a ticket first, then the socket with it.
func (l *line) connect(t *testing.T, operatorID uuid.UUID) *websocket.Conn {
	t.Helper()
	ticket, _, err := l.hub.Issue(operatorID)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(l.server.URL, "http")+"/inbox/stream?ticket="+ticket, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	l.dialed++
	l.awaitClients(t)
	return conn
}

// awaitClients waits until every socket dialed is registered, so a notice posted next cannot overtake one.
func (l *line) awaitClients(t *testing.T) {
	t.Helper()
	want := l.dialed
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		l.hub.mu.Lock()
		n := len(l.hub.clients)
		l.hub.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the sockets never registered")
}

type received struct {
	Type         string `json:"type"`
	ID           string `json:"id"`
	Conversation *struct {
		ID string `json:"id"`
	} `json:"conversation"`
}

func next(t *testing.T, conn *websocket.Conn) received {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var r received
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("the event is not json: %v - %s", err, raw)
	}
	return r
}

func (r received) about() string {
	if r.Conversation != nil {
		return r.Type + " " + r.Conversation.ID
	}
	return r.Type + " " + r.ID
}

// TestEachOperatorHearsWhatItsInboxShows is ADR-0035 on F21; a change all reach goes last, so no case ever waits on a silence.
func TestEachOperatorHearsWhatItsInboxShows(t *testing.T) {
	l := newLine(t)
	account, elsewhere := id.New(), id.New()
	holder := l.actor(account, enums.OperatorRoleOperator)
	vendor := l.actor(account, enums.OperatorRoleOperator)
	supervisor := l.actor(account, enums.OperatorRoleSupervisor)
	stranger := l.actor(elsewhere, enums.OperatorRoleAdmin)

	held := l.rows.put(account, &holder)
	pooled := l.rows.put(account, nil)
	strangersOwn := l.rows.put(elsewhere, nil)

	conns := map[string]*websocket.Conn{}
	for name, operatorID := range map[string]uuid.UUID{"holder": holder, "vendor": vendor, "supervisor": supervisor, "stranger": stranger} {
		conns[name] = l.connect(t, operatorID)
	}

	l.hub.Changed(held)
	l.hub.Changed(pooled)
	l.hub.Changed(strangersOwn)

	for name, want := range map[string][]string{
		"holder":     {"conversation.updated " + held.String(), "conversation.updated " + pooled.String()},
		"supervisor": {"conversation.updated " + held.String(), "conversation.updated " + pooled.String()},
		"vendor":     {"conversation.updated " + pooled.String()},
		"stranger":   {"conversation.updated " + strangersOwn.String()},
	} {
		for _, w := range want {
			if got := next(t, conns[name]).about(); got != w {
				t.Fatalf("%s heard %q, want %q", name, got, w)
			}
		}
	}
}

// TestTakingThePoolTellsTheOthersToDropTheRow: the vendor who lost sight of it gets only its id, and nothing of others (ADR-0051).
func TestTakingThePoolTellsTheOthersToDropTheRow(t *testing.T) {
	l := newLine(t)
	account := id.New()
	taker := l.actor(account, enums.OperatorRoleOperator)
	other := l.actor(account, enums.OperatorRoleOperator)
	taken := l.rows.put(account, &taker)
	pooled := l.rows.put(account, nil)

	conn := l.connect(t, other)
	l.connect(t, taker)

	l.hub.Changed(taken)
	l.hub.LeftPool(taken)
	l.hub.Changed(pooled)

	for _, want := range []string{"conversation.removed " + taken.String(), "conversation.updated " + pooled.String()} {
		if got := next(t, conn).about(); got != want {
			t.Fatalf("the other vendor heard %q, want %q", got, want)
		}
	}
}

// TestARowThatCannotBeReadTellsNobody: a gone contact or a failed read sends nothing, and the readable one last proves it.
func TestARowThatCannotBeReadTellsNobody(t *testing.T) {
	l := newLine(t)
	account := id.New()
	admin := l.actor(account, enums.OperatorRoleAdmin)
	broken := l.rows.put(account, nil)
	visible := l.rows.put(account, nil)
	l.rows.failing = map[uuid.UUID]bool{broken: true}
	conn := l.connect(t, admin)

	l.hub.Changed(id.New())
	l.hub.Changed(broken)
	l.hub.Changed(visible)

	if got := next(t, conn).about(); got != "conversation.updated "+visible.String() {
		t.Fatalf("heard %q first, want only the readable change", got)
	}
}

// TestATicketOpensOneSocketOnce: unknown, used, expired tickets and a disabled operator are refused before the upgrade (ADR-0051).
func TestATicketOpensOneSocketOnce(t *testing.T) {
	l := newLine(t)
	account := id.New()
	operatorID := l.actor(account, enums.OperatorRoleOperator)
	gate := l.server.URL + "/inbox/stream?ticket="

	get := func(ticket string) (int, string) {
		t.Helper()
		res, err := http.Get(gate + ticket)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer func() { _ = res.Body.Close() }()
		var body struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body.Error.Code
	}

	used, _, _ := l.hub.Issue(operatorID)
	if _, err := l.hub.Redeem(used); err != nil {
		t.Fatalf("a fresh ticket was refused: %v", err)
	}
	expired, _, _ := l.hub.Issue(operatorID)
	l.hub.mu.Lock()
	l.hub.tickets[expired] = ticket{operatorID: operatorID, expiresAt: time.Now().Add(-time.Second)}
	l.hub.mu.Unlock()
	disabled := l.actor(account, enums.OperatorRoleOperator)
	orphan, _, _ := l.hub.Issue(disabled)
	l.mu.Lock()
	delete(l.actors, disabled)
	l.mu.Unlock()

	for _, tc := range []struct {
		name, ticket, want string
	}{
		{"no ticket", "", "invalid_ticket"},
		{"a ticket never issued", "inventado", "invalid_ticket"},
		{"a ticket already used", used, "invalid_ticket"},
		{"an expired ticket", expired, "invalid_ticket"},
		{"an operator disabled after asking", orphan, "session_no_longer_valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, code := get(tc.ticket); status != http.StatusUnauthorized || code != tc.want {
				t.Fatalf("got %d %q, want 401 %q", status, code, tc.want)
			}
		})
	}
}

// TestTheTicketRouteIsBehindTheBearerForEveryRole, because a wrong Roles list compiles and that role would lose real time.
func TestTheTicketRouteIsBehindTheBearerForEveryRole(t *testing.T) {
	hub := NewHub(HubOptions{Rows: &stubRows{rows: map[uuid.UUID]stubRow{}}})
	t.Cleanup(func() { _ = hub.Close(context.Background()) })

	for _, role := range enums.OperatorRoleValues {
		t.Run(string(role), func(t *testing.T) {
			engine := soniahttp.NewRouter(soniahttp.RouterOptions{
				Controllers:   []soniahttp.Controller{NewStreamController(hub, nil)},
				TokenVerifier: func(string) (uuid.UUID, error) { return testOperator, nil },
				ActorLookup: func(context.Context, uuid.UUID) (soniahttp.Actor, error) {
					return soniahttp.Actor{OperatorID: testOperator, AccountID: testAccount, Role: role}, nil
				},
			})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/inbox/stream-ticket", nil)
			req.Header.Set("Authorization", "Bearer cualquier-cosa")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("got %d, want 201 - body %s", rec.Code, rec.Body)
			}
			var body ticketResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Ticket == "" {
				t.Fatalf("no ticket in %s", rec.Body)
			}
			if got, err := hub.Redeem(body.Ticket); err != nil || got != testOperator {
				t.Fatalf("the ticket handed out redeems as %v, %v", got, err)
			}
		})
	}

	// Without the bearer the router refuses before the handler, which is what keeps the ticket from being a way around it.
	engine := soniahttp.NewRouter(soniahttp.RouterOptions{Controllers: []soniahttp.Controller{NewStreamController(hub, nil)}})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/inbox/stream-ticket", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("without a bearer: got %d, want 401", rec.Code)
	}
}

// TestASlowClientIsCutAndNotWaitedFor: a full queue ends that one socket with policy violation, and the fan-out moves on.
func TestASlowClientIsCutAndNotWaitedFor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &client{send: make(chan []byte, 1), cancel: cancel}

	c.offer([]byte("uno"))
	done := make(chan struct{})
	go func() {
		c.offer([]byte("dos"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("offering to a full client blocked the fan-out")
	}
	if ctx.Err() == nil || c.status != websocket.StatusPolicyViolation {
		t.Fatalf("the slow client was not cut: ctx %v, status %v", ctx.Err(), c.status)
	}
}

// TestClosingTheHubSaysGoingAway, so the frontend reconnects after a deploy, and a later notice never blocks (ADR-0027, ADR-0051).
func TestClosingTheHubSaysGoingAway(t *testing.T) {
	l := newLine(t)
	conn := l.connect(t, l.actor(id.New(), enums.OperatorRoleAdmin))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Reading while the hub closes, as a browser does: the close handshake waits for the other side to answer it.
	ended := make(chan error, 1)
	go func() {
		_, _, err := conn.Read(ctx)
		ended <- err
	}()
	if err := l.hub.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-ended; websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("the socket ended with %v, want going away", err)
	}

	posted := make(chan struct{})
	go func() {
		l.hub.Changed(id.New())
		close(posted)
	}()
	select {
	case <-posted:
	case <-time.After(time.Second):
		t.Fatal("a notice after the close blocked its caller")
	}
}

// TestAReassignmentTellsOnlyTheFormerVendorToDropTheRow: a vendor who never saw it is told nothing (ADR-0035 regla 3, ADR-0056).
func TestAReassignmentTellsOnlyTheFormerVendorToDropTheRow(t *testing.T) {
	l := newLine(t)
	account := id.New()
	former := l.actor(account, enums.OperatorRoleOperator)
	bystander := l.actor(account, enums.OperatorRoleOperator)
	successor := l.actor(account, enums.OperatorRoleOperator)
	moved := l.rows.put(account, &successor)
	pooled := l.rows.put(account, nil)

	conns := map[string]*websocket.Conn{}
	for name, operatorID := range map[string]uuid.UUID{"former": former, "bystander": bystander, "successor": successor} {
		conns[name] = l.connect(t, operatorID)
	}

	l.hub.Reassigned(moved, former)
	l.hub.Changed(pooled)

	for name, want := range map[string][]string{
		"former":    {"conversation.removed " + moved.String(), "conversation.updated " + pooled.String()},
		"bystander": {"conversation.updated " + pooled.String()},
		"successor": {"conversation.updated " + moved.String(), "conversation.updated " + pooled.String()},
	} {
		for _, w := range want {
			if got := next(t, conns[name]).about(); got != w {
				t.Fatalf("%s heard %q, want %q", name, got, w)
			}
		}
	}
}

// TestAReminderReachesOnlyItsRecipient is Remind: the person the reminder goes to hears it, a teammate does not (ADR-0058).
func TestAReminderReachesOnlyItsRecipient(t *testing.T) {
	l := newLine(t)
	account := id.New()
	recipient := l.actor(account, enums.OperatorRoleOperator)
	teammate := l.actor(account, enums.OperatorRoleSupervisor)
	pooled := l.rows.put(account, nil)

	conns := map[string]*websocket.Conn{"recipient": l.connect(t, recipient), "teammate": l.connect(t, teammate)}

	l.hub.Remind(recipient, map[string]string{"id": "r1"})
	l.hub.Changed(pooled)

	for name, want := range map[string][]string{
		"recipient": {"reminder.due ", "conversation.updated " + pooled.String()},
		"teammate":  {"conversation.updated " + pooled.String()},
	} {
		for _, w := range want {
			if got := next(t, conns[name]).about(); got != w {
				t.Fatalf("%s heard %q, want %q", name, got, w)
			}
		}
	}
}
