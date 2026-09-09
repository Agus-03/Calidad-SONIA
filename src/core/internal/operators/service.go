package operators

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

var (
	// One answer for an unknown email, a wrong password and a disabled account: telling them apart enumerates who works here.
	ErrInvalidCredentials = errors.New("operators: invalid credentials")

	// Separate from the login answer because here the caller already proved they hold a token: nothing is leaked by being precise.
	ErrInactiveOperator = errors.New("operators: the operator is no longer active")
)

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	ByEmail(ctx context.Context, email string) (Operator, error)
	ByID(ctx context.Context, operatorID uuid.UUID) (Operator, error)
}

// issuer is the slice of platform/auth the service uses, so a login can be tested without signing anything.
type issuer interface {
	Issue(operatorID uuid.UUID, now time.Time) (string, time.Time, error)
}

type Service struct {
	repo   store
	tokens issuer
	log    *slog.Logger
}

func NewService(repo store, tokens issuer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, tokens: tokens, log: log}
}

// Session is what a successful login hands the controller: the operator travels so the response can name who came in.
type Session struct {
	Token     string
	ExpiresAt time.Time
	Operator  Operator
}

// Login refuses in three different ways and answers all three the same, with the real reason written to the log and never to the response.
func (s *Service) Login(ctx context.Context, email, password string, now time.Time) (Session, error) {
	operator, err := s.repo.ByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// Spends what a real verification spends, so an address nobody registered does not answer faster (ADR-0030, regla 8).
		WasteAComparison()
		s.log.Info("login refused", slog.String("reason", "unknown email"))
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}

	// Checked before active on purpose: refusing a disabled account without verifying would answer early and say the address exists.
	if err := Verify(operator.PasswordHash, password); err != nil {
		if errors.Is(err, ErrWrongPassword) {
			s.log.Info("login refused",
				slog.String("reason", "wrong password"), slog.String("operator_id", operator.ID.String()))
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, err
	}

	if !operator.Active {
		s.log.Info("login refused",
			slog.String("reason", "inactive operator"), slog.String("operator_id", operator.ID.String()))
		return Session{}, ErrInvalidCredentials
	}

	token, expiresAt, err := s.tokens.Issue(operator.ID, now)
	if err != nil {
		return Session{}, err
	}

	s.log.Info("login granted",
		slog.String("operator_id", operator.ID.String()), slog.String("role", string(operator.Role)))
	return Session{Token: token, ExpiresAt: expiresAt, Operator: operator}, nil
}

// Resolve runs on every authenticated request, which is what keeps role and active current instead of frozen at login (ADR-0030, regla 2).
func (s *Service) Resolve(ctx context.Context, operatorID uuid.UUID) (Operator, error) {
	operator, err := s.repo.ByID(ctx, operatorID)
	if err != nil {
		return Operator{}, err
	}
	if !operator.Active {
		return Operator{}, ErrInactiveOperator
	}
	return operator, nil
}
