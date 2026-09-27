package smoke

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// TestAWebhookReachesTheOpenInboxThroughTheRealProcess walks F21 as the frontend will: a ticket behind the bearer, the socket with
// it, and the customer writing is pushed to the supervisor's open inbox without asking. It fails if the hub is not wired into the
// process, or if the webhook stopped telling it once its transaction committed (ADR-0051).
func TestAWebhookReachesTheOpenInboxThroughTheRealProcess(t *testing.T) {
	core, brain, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)
	waitForHealth(t, brain)

	secret := signingSecret(ctx, t, pool)
	switchers := seedSwitchers(ctx, t, core, pool)
	conversation := demoConversation(ctx, t, core, pool, secret)

	// The same release as the switch case: the customer writing may hand the demo conversation to the seeded vendor (ADR-0037),
	// and its assignments would keep the vendor alive past its own cleanup.
	release := func() {
		if _, err := pool.Exec(context.Background(), `UPDATE conversations SET operator_id = NULL WHERE id = $1`, conversation); err != nil {
			t.Errorf("returning the demo conversation to the pool: %v", err)
		}
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM conversation_assignments WHERE conversation_id = $1 AND to_operator_id = ANY($2)`, conversation, switchers.seeded); err != nil {
			t.Errorf("cleaning up the assignments: %v", err)
		}
	}
	release()
	t.Cleanup(release)

	socket := openStream(ctx, t, core, switchers.supervisor)

	arrival := "wamid.smoke.stream." + uuid.New().String()
	if status := postWebhook(t, core, arrival, secret); status != http.StatusOK {
		t.Fatalf("POST /webhooks/whatsapp: got %d, want 200", status)
	}
	// Watched before the turn is awaited: the webhook's own event is the one F21 is about, not the one the turn sends after it.
	awaitEventAbout(ctx, t, socket, conversation.String())
	awaitTurn(ctx, t, pool, arrival)
}

// TestAStreamTicketOpensOneSocketOnce is the credential of ADR-0051 through the real process: the gate refuses a ticket it never
// issued and one already spent, before any upgrade.
func TestAStreamTicketOpensOneSocketOnce(t *testing.T) {
	core, _, pool := stack(t)
	ctx := context.Background()
	waitForHealth(t, core)

	switchers := seedSwitchers(ctx, t, core, pool)
	ticket := streamTicket(t, core, switchers.operator)
	openWith(ctx, t, core, ticket)

	for _, spent := range []string{ticket, "inventado"} {
		res, err := http.Get(core + "/inbox/stream?ticket=" + spent)
		if err != nil {
			t.Fatalf("GET /inbox/stream: %v", err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("the gate with ticket %q: got %d, want 401", spent, res.StatusCode)
		}
	}
}

func streamTicket(t *testing.T, core, token string) string {
	t.Helper()
	status, body := call(t, core, http.MethodPost, "/api/v1/inbox/stream-ticket", token, "")
	if status != http.StatusCreated {
		t.Fatalf("POST /api/v1/inbox/stream-ticket: got %d, want 201 - %s", status, body)
	}
	var issued struct {
		Ticket string `json:"ticket"`
	}
	if err := json.Unmarshal([]byte(body), &issued); err != nil || issued.Ticket == "" {
		t.Fatalf("no ticket in %s", body)
	}
	return issued.Ticket
}

func openStream(ctx context.Context, t *testing.T, core, token string) *websocket.Conn {
	t.Helper()
	return openWith(ctx, t, core, streamTicket(t, core, token))
}

func openWith(ctx context.Context, t *testing.T, core, ticket string) *websocket.Conn {
	t.Helper()
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(core, "http")+"/inbox/stream?ticket="+ticket, nil)
	if err != nil {
		t.Fatalf("opening the inbox stream: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// awaitEventAbout reads the socket until an event names the conversation: others of the stack may be written at the same time.
func awaitEventAbout(ctx context.Context, t *testing.T, conn *websocket.Conn, conversationID string) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		_, raw, err := conn.Read(readCtx)
		if err != nil {
			t.Fatalf("no event about %s reached the open inbox: %v", conversationID, err)
		}
		var event struct {
			Type         string `json:"type"`
			Conversation *struct {
				ID string `json:"id"`
			} `json:"conversation"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatalf("the event is not json: %v - %s", err, raw)
		}
		if event.Type == "conversation.updated" && event.Conversation != nil && event.Conversation.ID == conversationID {
			return
		}
	}
}
