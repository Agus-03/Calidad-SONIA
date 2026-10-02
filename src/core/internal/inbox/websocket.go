package inbox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// ErrInvalidTicket covers a ticket never issued, one already used and one expired: telling them apart would only help a guesser.
var ErrInvalidTicket = errors.New("inbox: the stream ticket is not valid")

const (
	// Long enough for the frontend to open the socket right after asking, short enough that a ticket leaked in a log is dead (ADR-0051).
	ticketTTL = 30 * time.Second
	// A burst of changes fits; a client that falls this far behind is cut and refetches, instead of holding the others back.
	clientBuffer = 32
	// The changes waiting to be read and sent. Past this the hub drops and logs: a webhook never waits on the inbox (ADR-0029).
	noticeBuffer = 256
	// Under the idle timeout of the usual proxies, so an open inbox with nothing happening is not cut in the middle.
	pingInterval = 25 * time.Second
	writeTimeout = 5 * time.Second
	// The read of the row that goes out with each event, bounded so one slow query cannot stall every notice behind it.
	readTimeout = 5 * time.Second
)

// rows is the one read the hub makes: the inbox row as the listing draws it, plus the account it belongs to.
type rows interface {
	SummaryOf(ctx context.Context, conversationID uuid.UUID) (uuid.UUID, Summary, error)
}

type HubOptions struct {
	Rows   rows
	Logger *slog.Logger
}

// Hub is the fan-out of F21: the operators connected to this process, and what changed in the conversations they reach. It lives in
// memory because the core runs as one instance (ADR-0027); a second one would not reach the operators connected to the first.
type Hub struct {
	opts HubOptions

	mu      sync.Mutex
	clients map[*client]struct{}
	tickets map[string]ticket

	notices chan notice
	stop    chan struct{}
	once    sync.Once
	// The worker and every connection, so Close can wait until nothing of the hub is left running.
	running sync.WaitGroup
}

type ticket struct {
	operatorID uuid.UUID
	expiresAt  time.Time
}

// notice is one conversation that changed. leftPool marks the one change that takes a row away from somebody's inbox: a
// conversation of the pool taken by another operator (ADR-0051).
type notice struct {
	conversationID uuid.UUID
	leftPool       bool
	// The vendor a reassignment took the conversation from, the one other person who had the row on screen (ADR-0056).
	formerHolder *uuid.UUID
}

func NewHub(opts HubOptions) *Hub {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	h := &Hub{
		opts:    opts,
		clients: map[*client]struct{}{},
		tickets: map[string]ticket{},
		notices: make(chan notice, noticeBuffer),
		stop:    make(chan struct{}),
	}
	h.running.Add(1)
	go h.work()
	return h
}

// Issue hands out a ticket for the socket: the browser cannot send the bearer when it opens one, and the JWT in the URL would end up
// in every access log on the way (ADR-0051).
func (h *Hub) Issue(operatorID uuid.UUID) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(ticketTTL)

	h.mu.Lock()
	defer h.mu.Unlock()
	// Swept here and not by a timer: the map only grows when somebody asks, so asking is when it is worth pruning.
	now := time.Now()
	for t, issued := range h.tickets {
		if now.After(issued.expiresAt) {
			delete(h.tickets, t)
		}
	}
	h.tickets[token] = ticket{operatorID: operatorID, expiresAt: expiresAt}
	return token, expiresAt, nil
}

// Redeem spends the ticket whatever happens next: one use, so a ticket read off a log after the socket opened is worth nothing.
func (h *Hub) Redeem(token string) (uuid.UUID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	issued, ok := h.tickets[token]
	delete(h.tickets, token)
	if !ok || time.Now().After(issued.expiresAt) {
		return uuid.Nil, ErrInvalidTicket
	}
	return issued.operatorID, nil
}

// Changed is what a package that writes a conversation says after its transaction commits. Never inside it: a rollback would push
// a change that did not happen (ADR-0051). It never blocks the caller.
func (h *Hub) Changed(conversationID uuid.UUID) { h.post(notice{conversationID: conversationID}) }

// LeftPool is Changed for a conversation that was in the pool and is somebody's now: the operators who can no longer see it are
// told to drop its row, which every one of them had in front of them a moment ago (ADR-0035, regla 1).
func (h *Hub) LeftPool(conversationID uuid.UUID) {
	h.post(notice{conversationID: conversationID, leftPool: true})
}

// Reassigned is Changed for a conversation F26 moved: only its former vendor is told to drop the row (ADR-0035 regla 3, ADR-0056).
func (h *Hub) Reassigned(conversationID, formerHolder uuid.UUID) {
	h.post(notice{conversationID: conversationID, formerHolder: &formerHolder})
}

func (h *Hub) post(n notice) {
	select {
	case <-h.stop:
	case h.notices <- n:
	default:
		// Dropped and not waited for: the caller may be the webhook, and Meta is waiting on its 200 (ADR-0029).
		h.opts.Logger.Warn("inbox event dropped, the queue is full", slog.String("conversation_id", n.conversationID.String()))
	}
}

func (h *Hub) work() {
	defer h.running.Done()
	for {
		select {
		case <-h.stop:
			return
		case n := <-h.notices:
			h.fanOut(n)
		}
	}
}

type conversationEvent struct {
	Type         string           `json:"type"`
	Conversation *summaryResponse `json:"conversation,omitempty"`
	ID           string           `json:"id,omitempty"`
}

// fanOut reads the row once and decides per connection, with the reach of the listing: an operator never receives a conversation
// it could not open (ADR-0035, reglas 1 y 3).
func (h *Hub) fanOut(n notice) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	accountID, summary, err := h.opts.Rows.SummaryOf(ctx, n.conversationID)
	if errors.Is(err, ErrNotFound) {
		// A contact taken down takes its conversation out of every inbox, so nobody is told about it (ADR-0035, regla 1).
		return
	}
	if err != nil {
		h.opts.Logger.Error("inbox event not sent, the row could not be read",
			slog.String("conversation_id", n.conversationID.String()), slog.String("cause", err.Error()))
		return
	}

	row := present(summary)
	updated, err := json.Marshal(conversationEvent{Type: "conversation.updated", Conversation: &row})
	if err != nil {
		h.opts.Logger.Error("inbox event not encoded", slog.String("cause", err.Error()))
		return
	}
	removed, err := json.Marshal(conversationEvent{Type: "conversation.removed", ID: n.conversationID.String()})
	if err != nil {
		h.opts.Logger.Error("inbox event not encoded", slog.String("cause", err.Error()))
		return
	}

	var holder *uuid.UUID
	if summary.Operator != nil {
		holder = &summary.Operator.ID
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.scope.AccountID != accountID {
			continue
		}
		switch {
		case canSee(c.scope, holder):
			c.offer(updated)
		case n.leftPool:
			c.offer(removed)
		case n.formerHolder != nil && *n.formerHolder == c.scope.OperatorID:
			c.offer(removed)
		}
	}
}

// client is one open socket. Its queue is the only thing the hub touches, so a slow browser can never stall the fan-out.
type client struct {
	scope Scope
	send  chan []byte

	once   sync.Once
	cancel context.CancelFunc
	status websocket.StatusCode
	reason string
}

// offer never blocks: a client whose queue is full is cut, and its frontend refetches the first page when it reconnects (ADR-0051).
func (c *client) offer(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.end(websocket.StatusPolicyViolation, "too slow to keep up with the inbox")
	}
}

func (c *client) end(status websocket.StatusCode, reason string) {
	c.once.Do(func() {
		c.status, c.reason = status, reason
		c.cancel()
	})
}

// Serve holds one socket until the browser leaves, the hub closes or the client falls behind, and it is the only writer of it.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, scope Scope) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := &client{scope: scope, send: make(chan []byte, clientBuffer), cancel: cancel, status: websocket.StatusNormalClosure}

	h.mu.Lock()
	select {
	case <-h.stop:
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusGoingAway, "the server is shutting down")
		return
	default:
	}
	h.clients[c] = struct{}{}
	h.running.Add(1)
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		h.running.Done()
	}()

	// The browser sends nothing on this socket: reading is only how its close and the pongs are heard. On a context of its own,
	// because CloseRead drops the connection without a close frame when its context ends, and ending ours has to say why.
	left := conn.CloseRead(context.WithoutCancel(ctx))
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-left.Done():
			return
		case <-ctx.Done():
			// Through the once, so a slow or closing end that raced the browser leaving is read whole, and a plain close is the default.
			c.end(websocket.StatusNormalClosure, "")
			_ = conn.Close(c.status, c.reason)
			return
		case msg := <-c.send:
			if err := write(ctx, conn, msg); err != nil {
				return
			}
		case <-ping.C:
			pingCtx, stop := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pingCtx)
			stop()
			if err != nil {
				return
			}
		}
	}
}

func write(ctx context.Context, conn *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg)
}

// Close ends every socket with going away, so the frontend knows to reconnect, and waits for the worker and the sockets to stop.
func (h *Hub) Close(ctx context.Context) error {
	h.once.Do(func() {
		h.mu.Lock()
		close(h.stop)
		for c := range h.clients {
			c.end(websocket.StatusGoingAway, "the server is shutting down")
		}
		h.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		h.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
