package whatsapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// rollback ends every case: the assertions run against real DDL and leave the database as they found it.
var rollback = errors.New("whatsapp: rolling back the integration case")

func integrationPool(t *testing.T) db.Pool {
	t.Helper()

	dsn := os.Getenv("SONIA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("SONIA_TEST_DATABASE_URL is not set: bring the stack up and run make test-integration")
	}

	pool, err := db.Open(context.Background(), db.Options{DatabaseURL: dsn, MinSize: 1, MaxSize: 4})
	if err != nil {
		t.Fatalf("Open: got %v, want no error", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// An inbox hangs from an account, and accounts holds a single row for the whole project (ADR-0012).
func seededAccount(t *testing.T, pool db.Pool) uuid.UUID {
	t.Helper()

	var account uuid.UUID
	if err := pool.QueryRow(context.Background(), `SELECT id FROM accounts LIMIT 1`).Scan(&account); err != nil {
		t.Skip("there is no account: run core --seed-admin first")
	}
	return account
}

func testCipher(t *testing.T) *encryption.Cipher {
	t.Helper()

	key := make([]byte, encryption.KeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	cipher, err := encryption.New(key)
	if err != nil {
		t.Fatalf("encryption.New: %v", err)
	}
	return cipher
}

// seededInbox is the row this case owns: unique ids and tokens, so it never answers for the demo inbox or another case.
type seededInbox struct {
	id            uuid.UUID
	phoneNumberID string
	verifyToken   string
}

func insertInbox(ctx context.Context, t *testing.T, exec db.Executor, account uuid.UUID, accessToken, appSecret []byte) seededInbox {
	t.Helper()

	row := seededInbox{id: id.New(), phoneNumberID: "it-" + id.New().String(), verifyToken: "it-" + id.New().String()}
	_, err := exec.Exec(ctx, `
		INSERT INTO inboxes (id, account_id, phone_number_id, waba_id, display_name,
		                     access_token_encrypted, verify_token, app_secret_encrypted, status)
		VALUES ($1, $2, $3, 'it-waba', 'Numero de integracion', $4, $5, $6, 'active')`,
		row.id, account, row.phoneNumberID, accessToken, row.verifyToken, appSecret)
	if err != nil {
		t.Fatalf("seed the inbox: %v", err)
	}
	return row
}

func encrypt(t *testing.T, cipher *encryption.Cipher, clear string) []byte {
	t.Helper()

	stored, err := cipher.Encrypt(clear)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	return stored
}

// TestTheInboxSecretsAreUnreadableInTheDatabase is ADR-0031 for the two columns of inboxes: a dump carries neither Meta's token
// nor the key that signs every webhook, and the three reads of the channel hand them back in the clear.
func TestTheInboxSecretsAreUnreadableInTheDatabase(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)
	cipher := testCipher(t)

	const token, secret = "EAAG-token-de-integracion", "app-secret-de-integracion"

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, cipher)
		seeded := insertInbox(ctx, t, exec, account, encrypt(t, cipher, token), encrypt(t, cipher, secret))

		var rawToken, rawSecret []byte
		if err := exec.QueryRow(ctx, `SELECT access_token_encrypted, app_secret_encrypted FROM inboxes WHERE id = $1`, seeded.id).
			Scan(&rawToken, &rawSecret); err != nil {
			t.Fatalf("read the raw columns: %v", err)
		}
		if bytes.Contains(rawToken, []byte(token)) || bytes.Contains(rawSecret, []byte(secret)) {
			t.Fatalf("a secret of the inbox is in the database in the clear")
		}

		reads := map[string]func() (Inbox, error){
			"by phone number id": func() (Inbox, error) { return repo.InboxByPhoneNumberID(ctx, seeded.phoneNumberID) },
			"by verify token":    func() (Inbox, error) { return repo.InboxByVerifyToken(ctx, seeded.verifyToken) },
			"by id":              func() (Inbox, error) { return repo.InboxByID(ctx, seeded.id) },
		}
		for name, read := range reads {
			got, err := read()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if got.ID != seeded.id || got.AccessToken != token || got.AppSecret != secret {
				t.Fatalf("%s read id=%v token=%q secret=%q, want %v and the two in the clear",
					name, got.ID, got.AccessToken, got.AppSecret, seeded.id)
			}
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestAnInboxNobodyConnectedReadsWithoutSecrets protects the meaning NULL took in 0001: a number with no credentials is found,
// and its empty secret is what makes every webhook it receives a forgery instead of a crash.
func TestAnInboxNobodyConnectedReadsWithoutSecrets(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		seeded := insertInbox(ctx, t, exec, account, nil, nil)

		got, err := NewRepository(exec, testCipher(t)).InboxByPhoneNumberID(ctx, seeded.phoneNumberID)
		if err != nil {
			t.Fatalf("InboxByPhoneNumberID: %v", err)
		}
		if got.AccessToken != "" || got.AppSecret != "" {
			t.Fatalf("NULL secrets read as token=%q secret=%q, want both empty", got.AccessToken, got.AppSecret)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}

// TestASecretUnderAnotherKeyRefusesTheInbox keeps a lost or rotated key from reading as a wrong signature: checking webhooks
// against garbage would discard every real message in silence (ADR-0031, regla 2).
func TestASecretUnderAnotherKeyRefusesTheInbox(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	for _, tc := range []struct {
		name        string
		accessToken bool
	}{
		{"the access token", true},
		{"the app secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.InTx(ctx, pool, func(exec db.Executor) error {
				foreign := encrypt(t, testCipher(t), "cifrado-con-otra-llave")
				token, secret := foreign, []byte(nil)
				if !tc.accessToken {
					token, secret = nil, foreign
				}
				seeded := insertInbox(ctx, t, exec, account, token, secret)

				_, err := NewRepository(exec, testCipher(t)).InboxByID(ctx, seeded.id)
				if !errors.Is(err, encryption.ErrCorrupted) {
					t.Fatalf("InboxByID: got %v, want ErrCorrupted", err)
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("InTx: got %v, want the rollback sentinel", err)
			}
		})
	}
}

// TestADiscardedEventIsKeptAsTheSchemaDeclares writes the row a forgery leaves, so the CHECK of webhook_events and the
// repository agree on it: signature_valid false, status discarded, and no processed_at (F20).
func TestADiscardedEventIsKeptAsTheSchemaDeclares(t *testing.T) {
	ctx := context.Background()
	pool := integrationPool(t)
	account := seededAccount(t, pool)

	err := db.InTx(ctx, pool, func(exec db.Executor) error {
		repo := NewRepository(exec, testCipher(t))
		seeded := insertInbox(ctx, t, exec, account, nil, nil)
		wamid := "wamid.it." + id.New().String()

		event := WebhookEvent{
			ID: id.New(), InboxID: seeded.id, EventType: enums.WebhookEventTypeMessage, WAMID: &wamid,
			PayloadJSON: []byte(metaTextPayload), SignatureValid: false, Status: enums.WebhookEventStatusDiscarded,
			ReceivedAt: time.Now().UTC(),
		}
		if err := repo.SaveWebhookEvent(ctx, event); err != nil {
			t.Fatalf("SaveWebhookEvent: %v", err)
		}

		var valid, processed bool
		var status string
		if err := exec.QueryRow(ctx,
			`SELECT signature_valid, status, processed_at IS NOT NULL FROM webhook_events WHERE id = $1`, event.ID).
			Scan(&valid, &status, &processed); err != nil {
			t.Fatalf("read the event back: %v", err)
		}
		if valid || status != string(enums.WebhookEventStatusDiscarded) || processed {
			t.Fatalf("stored signature_valid=%v status=%q processed=%v, want false, discarded and not processed", valid, status, processed)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("InTx: got %v, want the rollback sentinel", err)
	}
}
