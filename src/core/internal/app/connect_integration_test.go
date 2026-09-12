package app

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// connectKey is any 32 bytes: the case only needs the command and the read below to agree on one (ADR-0031, regla 7).
var connectKey = make([]byte, encryption.KeyBytes)

// connectEnv sets what the command reads plus what config demands of any boot, and inserts the row the command needs to find.
// The row is written for real and not in a transaction, because the command opens its own pool, so the case removes it itself.
func connectEnv(t *testing.T) (phoneNumberID string, inboxID uuid.UUID) {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	t.Setenv("DATABASE_URL", dsn)
	t.Setenv("BRAIN_BASE_URL", "http://127.0.0.1:8000")
	t.Setenv("BRAIN_INTERNAL_TOKEN", "test-token")
	t.Setenv("JWT_SECRET", "test-secret-that-is-long-enough-for-hs256")
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(connectKey))

	ctx := context.Background()
	pool := seedPool(t, ctx)

	var account uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}

	phoneNumberID, inboxID = "it-connect-"+id.New().String(), id.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO inboxes (id, account_id, phone_number_id, waba_id, display_name, verify_token, status)
		VALUES ($1, $2, $3, 'it-waba', 'Numero a conectar', $4, 'active')`,
		inboxID, account, phoneNumberID, "it-"+id.New().String())
	if err != nil {
		t.Fatalf("seed the inbox: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM inboxes WHERE id = $1`, inboxID); err != nil {
			t.Errorf("removing the inbox: %v", err)
		}
	})

	t.Setenv("CONNECT_INBOX_PHONE_NUMBER_ID", phoneNumberID)
	t.Setenv("CONNECT_INBOX_ACCESS_TOKEN", "EAAG-token-del-comando")
	t.Setenv("CONNECT_INBOX_APP_SECRET", "secreto-del-comando")
	return phoneNumberID, inboxID
}

// TestTheConnectedInboxIsOneTheWebhookCanCheck is the point of the command: what it writes is what the channel decrypts, with
// the key the process was given and not one of its own.
func TestTheConnectedInboxIsOneTheWebhookCanCheck(t *testing.T) {
	ctx := context.Background()
	phoneNumberID, inboxID := connectEnv(t)

	if err := ConnectInbox(ctx); err != nil {
		t.Fatalf("ConnectInbox: got %v, want no error", err)
	}

	cipher, err := encryption.New(connectKey)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	got, err := whatsapp.NewRepository(seedPool(t, ctx), cipher).InboxByPhoneNumberID(ctx, phoneNumberID)
	if err != nil {
		t.Fatalf("reading the connected inbox: %v", err)
	}
	if got.ID != inboxID || got.AccessToken != "EAAG-token-del-comando" || got.AppSecret != "secreto-del-comando" {
		t.Fatalf("read id=%v token=%q secret=%q, want %v and the two values of the environment",
			got.ID, got.AccessToken, got.AppSecret, inboxID)
	}
}

// TestConnectingWithoutASecretRefusesBeforeTouchingTheDatabase guards the half-connected number: a token with no secret
// would discard every webhook it receives as forged.
func TestConnectingWithoutASecretRefusesBeforeTouchingTheDatabase(t *testing.T) {
	ctx := context.Background()
	phoneNumberID, _ := connectEnv(t)
	t.Setenv("CONNECT_INBOX_APP_SECRET", "   ")

	if err := ConnectInbox(ctx); err == nil {
		t.Fatal("ConnectInbox with no app secret: got no error, want a refusal")
	}

	var tokenIsNull bool
	if err := seedPool(t, ctx).QueryRow(ctx,
		`SELECT access_token_encrypted IS NULL FROM inboxes WHERE phone_number_id = $1`, phoneNumberID).Scan(&tokenIsNull); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if !tokenIsNull {
		t.Fatal("the refusal still wrote the access token")
	}
}

// TestConnectingANumberWithNoRowSaysSo keeps a typo in the phone number id from reading as a success.
func TestConnectingANumberWithNoRowSaysSo(t *testing.T) {
	connectEnv(t)
	t.Setenv("CONNECT_INBOX_PHONE_NUMBER_ID", "it-sin-fila-"+id.New().String())

	if err := ConnectInbox(context.Background()); !errors.Is(err, whatsapp.ErrInboxNotFound) {
		t.Fatalf("ConnectInbox on a number with no row: got %v, want ErrInboxNotFound", err)
	}
}
