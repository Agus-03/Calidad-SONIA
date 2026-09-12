package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/encryption"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/logging"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// ConnectInbox writes the two Meta credentials of a number whose row already exists, and is run by the Makefile, never at boot (regla 3).
// SQL cannot encrypt, so until the inbox ABM exists this is the only way a real number gets its secrets (ADR-0031).
func ConnectInbox(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(log)

	phoneNumberID := strings.TrimSpace(cfg.ConnectInboxPhoneNumberID)
	accessToken := strings.TrimSpace(cfg.ConnectInboxAccessToken)
	appSecret := strings.TrimSpace(cfg.ConnectInboxAppSecret)
	// Both secrets or none: without the secret every webhook is discarded as forged, and without the token no answer ever leaves.
	if phoneNumberID == "" || accessToken == "" || appSecret == "" {
		return errors.New("app: CONNECT_INBOX_PHONE_NUMBER_ID, CONNECT_INBOX_ACCESS_TOKEN and CONNECT_INBOX_APP_SECRET are all required to connect an inbox")
	}

	cipher, err := encryption.New(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("app: the encryption key config accepted is not one the cipher can use: %w", err)
	}

	pool, err := db.Open(ctx, db.Options{
		DatabaseURL: cfg.DatabaseURL,
		MinSize:     cfg.DBPoolMinSize,
		MaxSize:     cfg.DBPoolMaxSize,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	inboxID, err := whatsapp.NewRepository(pool, cipher).Connect(ctx, phoneNumberID, accessToken, appSecret)
	if errors.Is(err, whatsapp.ErrInboxNotFound) {
		return fmt.Errorf("app: no inbox has phone_number_id %q, insert its row first: %w", phoneNumberID, err)
	}
	if err != nil {
		return err
	}

	// The secrets are never logged: the id is enough to find the row, and more people read the log than the .env.
	log.Info("inbox connected", slog.String("inbox_id", inboxID.String()), slog.String("phone_number_id", phoneNumberID))
	return nil
}
