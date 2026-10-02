package conversations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestTheOutboundMessageIsWrittenBeforeItIsSent is the operation the schema forbade until the nullable fix, and the reason for it.
func TestTheOutboundMessageIsWrittenBeforeItIsSent(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		service := NewService(repo, nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}

		at := time.Now().UTC()
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, at)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		if _, err := service.RecordInbound(ctx, conversation, "wamid."+id.New().String(), "hola", enums.MessageContentTypeText, at); err != nil {
			t.Fatalf("RecordInbound: %v", err)
		}

		draft := service.DraftOutbound(conversation.ID, agent, "buenas, en que puedo ayudarte", at)
		if err := repo.AppendMessage(ctx, draft); err != nil {
			t.Fatalf("the outbound message could not be written before being sent: %v", err)
		}

		var wamid *string
		var status *string
		if err := exec.QueryRow(ctx, `SELECT wamid, status FROM messages WHERE id = $1`, draft.ID).Scan(&wamid, &status); err != nil {
			t.Fatalf("read back the draft: %v", err)
		}
		if wamid != nil || status != nil {
			t.Fatalf("the draft was stored already sent: wamid %v status %v", wamid, status)
		}

		if err := repo.MarkSent(ctx, draft.ID, "wamid.sent", at); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		if err := exec.QueryRow(ctx, `SELECT wamid, status FROM messages WHERE id = $1`, draft.ID).Scan(&wamid, &status); err != nil {
			t.Fatalf("read back after sending: %v", err)
		}
		if wamid == nil || *wamid != "wamid.sent" || status == nil || *status != string(enums.MessageStatusSent) {
			t.Fatalf("Meta's answer did not land on the row: wamid %v status %v", wamid, status)
		}

		// ADR-0020 sends at most once, so a rejected send has to stay readable instead of being retried or removed.
		failed := service.DraftOutbound(conversation.ID, agent, "esta no sale", at)
		if err := repo.WithExecutor(exec).AppendMessage(ctx, failed); err != nil {
			t.Fatalf("append the message that will fail: %v", err)
		}
		if err := repo.MarkFailed(ctx, failed.ID, "131047"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		var code *string
		if err := exec.QueryRow(ctx, `SELECT status, error_code FROM messages WHERE id = $1`, failed.ID).Scan(&status, &code); err != nil {
			t.Fatalf("read back the failed message: %v", err)
		}
		if status == nil || *status != string(enums.MessageStatusFailed) || code == nil || *code != "131047" {
			t.Fatalf("the failed send left no visible trace: status %v code %v", status, code)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestTheFirstSendIsTheFirstResponse proves the three columns an answer moves: the operator's row passes the sender check, the
// activity moves when it is written, and first_response_at takes the first send only — never a failed one, never a later one.
func TestTheFirstSendIsTheFirstResponse(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account, inbox, agent := seeded(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec)
		service := NewService(repo, nil)

		contact, err := insertContact(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert contact: %v", err)
		}
		operator, err := insertOperator(ctx, exec, account)
		if err != nil {
			t.Fatalf("insert operator: %v", err)
		}
		// Well before the database clock, so "the activity moved" cannot pass by the two instants happening to coincide.
		opened := time.Date(2026, 1, 12, 10, 0, 0, 0, time.UTC)
		conversation, err := service.EnsureOpen(ctx, contact, inbox, &agent, true, opened)
		if err != nil {
			t.Fatalf("EnsureOpen: %v", err)
		}
		if _, err := exec.Exec(ctx, `UPDATE conversations SET last_activity_at = $2 WHERE id = $1`, conversation.ID, opened); err != nil {
			t.Fatalf("pin the activity: %v", err)
		}

		failed, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "esta no sale", opened)
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: the sender check refused the operator's row: %v", err)
		}
		if err := service.MarkFailed(ctx, failed.ID, "131047"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		var activity time.Time
		var firstResponse *time.Time
		read := func() {
			t.Helper()
			if err := exec.QueryRow(ctx, `SELECT last_activity_at, first_response_at FROM conversations WHERE id = $1`, conversation.ID).
				Scan(&activity, &firstResponse); err != nil {
				t.Fatalf("read the conversation: %v", err)
			}
		}
		read()
		if !activity.After(opened) {
			t.Fatalf("writing the answer left last_activity_at at %v", activity)
		}
		if firstResponse != nil {
			t.Fatalf("a failed send was taken as the first response: %v", *firstResponse)
		}

		sentAt := opened.Add(time.Minute)
		answer, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "ahora sí", sentAt)
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, answer.ID, "wamid.first", sentAt); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		read()
		// The database clock and not the caller's sentAt: F34 subtracts created_at from it, which is that clock too (C06).
		if firstResponse == nil || firstResponse.Equal(sentAt) || !firstResponse.After(opened) {
			t.Fatalf("first_response_at is %v, want the database clock at the first send, not the caller's %v", firstResponse, sentAt)
		}
		// now() does not move inside one transaction, so a second send would write the same instant: pinned to another one, the
		// comparison below can tell a kept value from a rewritten one.
		first := opened.Add(time.Second)
		if _, err := exec.Exec(ctx, `UPDATE conversations SET first_response_at = $2 WHERE id = $1`, conversation.ID, first); err != nil {
			t.Fatalf("pin the first response: %v", err)
		}

		later, err := service.RecordOperatorOutbound(ctx, conversation.ID, operator, "algo más", sentAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("RecordOperatorOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, later.ID, "wamid.second", sentAt.Add(time.Hour)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		read()
		if !firstResponse.Equal(first) {
			t.Fatalf("a later send moved first_response_at from %v to %v", first, *firstResponse)
		}

		// Decision O2 through the column F34 reads: the fixed text of a cut turn goes out like any other message and answers
		// nothing the customer asked, so a conversation whose only outbound row is that text has no response time at all.
		if _, err := exec.Exec(ctx, `UPDATE conversations SET first_response_at = NULL WHERE id = $1`, conversation.ID); err != nil {
			t.Fatalf("clear the first response: %v", err)
		}
		notice, err := service.RecordSystemOutbound(ctx, conversation.ID, "prefiero que esto lo vea una persona", sentAt)
		if err != nil {
			t.Fatalf("RecordSystemOutbound: %v", err)
		}
		if err := service.MarkSent(ctx, notice.ID, "wamid.notice", sentAt); err != nil {
			t.Fatalf("MarkSent on the fixed text: %v", err)
		}
		read()
		if firstResponse != nil {
			t.Fatalf("the fixed text of a cut turn was taken as the first response: %v", *firstResponse)
		}

		if err := service.MarkSent(ctx, id.New(), "wamid.nobody", sentAt); !errors.Is(err, ErrNotFound) {
			t.Fatalf("MarkSent on a message that does not exist: got %v, want ErrNotFound", err)
		}
		if err := repo.TouchOutbound(ctx, id.New()); !errors.Is(err, ErrNotFound) {
			t.Fatalf("TouchOutbound on a conversation that does not exist: got %v, want ErrNotFound", err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
