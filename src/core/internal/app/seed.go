package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/operators"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/logging"
)

// The first admin can rename itself once Fase 11 builds the screen: until then a fixed pair beats one more environment variable.
const (
	seedAdminName     = "Admin"
	seedAdminLastname = "Inicial"
)

// SeedAdmin is the only way into a database whose operators table is empty, and it is run by the Makefile and never at boot (regla 3).
func SeedAdmin(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, cfg.Environment)
	slog.SetDefault(log)

	email := strings.TrimSpace(cfg.SeedAdminEmail)
	// Refused rather than defaulted, because an admin with an empty password is a door and not a seed.
	if email == "" || cfg.SeedAdminPassword == "" {
		return errors.New("app: SEED_ADMIN_EMAIL and SEED_ADMIN_PASSWORD are both required to seed the admin")
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

	people := operators.NewRepository(pool)

	// Running it twice is a normal thing to do, and the second run must not reset a password that has since been changed.
	switch _, err := people.ByEmail(ctx, email); {
	case err == nil:
		log.Info("seed admin already there, nothing written", slog.String("email", email))
		return nil
	case !errors.Is(err, operators.ErrNotFound):
		return err
	}

	// Hashed here and never in SQL: the seed has to produce the very hash the login knows how to verify (ADR-0030).
	hash, err := operators.Hash(cfg.SeedAdminPassword)
	if err != nil {
		return fmt.Errorf("app: hash the seeded password: %w", err)
	}

	// One transaction, because an account without its admin is an installation nobody can log into (regla 5).
	err = db.InTx(ctx, pool, func(exec db.Executor) error {
		accountID, err := installAccount(ctx, exec, cfg.SeedAdminAccountName)
		if err != nil {
			return err
		}
		return people.WithExecutor(exec).Create(ctx, operators.Operator{
			ID:           id.New(),
			AccountID:    accountID,
			Name:         seedAdminName,
			Lastname:     seedAdminLastname,
			Email:        email,
			PasswordHash: hash,
			// Fixed and not configurable: seeding anyone below admin leaves nobody able to promote them (F18, Fase 11).
			Role:   enums.OperatorRoleAdmin,
			Active: true,
		})
	})
	if err != nil {
		return err
	}

	log.Info("seed admin created", slog.String("email", email))
	return nil
}

// No package owns accounts yet, so the row is written here: whoever installs is who creates the account it all hangs from.
func installAccount(ctx context.Context, exec db.Executor, name string) (uuid.UUID, error) {
	// The same guarded insert the demo seed uses, because the single-row invariant is a unique index that ON CONFLICT cannot infer.
	_, err := exec.Exec(ctx,
		`INSERT INTO accounts (id, name) SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM accounts)`,
		id.New(), strings.TrimSpace(name))
	if err != nil {
		return uuid.Nil, fmt.Errorf("app: create the account: %w", err)
	}

	var accountID uuid.UUID
	if err := exec.QueryRow(ctx, `SELECT id FROM accounts`).Scan(&accountID); err != nil {
		return uuid.Nil, fmt.Errorf("app: read the account: %w", err)
	}
	return accountID, nil
}
