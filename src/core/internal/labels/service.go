package labels

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// ErrEmptyName is refused before the database sees it: the column is NOT NULL but blanks would satisfy it.
var ErrEmptyName = errors.New("labels: the name is empty")

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	List(ctx context.Context, accountID uuid.UUID, limit, offset int) (Page, error)
	ByID(ctx context.Context, accountID, labelID uuid.UUID) (Label, error)
	Create(ctx context.Context, l Label) error
	Update(ctx context.Context, accountID, labelID uuid.UUID, name, color string) error
	SoftDelete(ctx context.Context, accountID, labelID uuid.UUID) error
}

type Service struct {
	repo store
	log  *slog.Logger
}

func NewService(repo store, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, log: log}
}

func (s *Service) List(ctx context.Context, accountID uuid.UUID, limit, offset int) (Page, error) {
	return s.repo.List(ctx, accountID, limit, offset)
}

// Create returns the row it wrote so the response can carry the id the caller did not choose (C04).
func (s *Service) Create(ctx context.Context, accountID, operatorID uuid.UUID, name, color string) (Label, error) {
	if err := validateName(name); err != nil {
		return Label{}, err
	}

	label := Label{
		ID: id.New(), AccountID: accountID,
		Name: normalizeName(name), Color: color, CreatedByOperatorID: operatorID,
	}
	if err := s.repo.Create(ctx, label); err != nil {
		return Label{}, err
	}

	s.log.Info("label created",
		slog.String("label_id", label.ID.String()), slog.String("operator_id", operatorID.String()))
	return label, nil
}

// Update reads the row back because the repository reports what changed and not what the row now says.
func (s *Service) Update(ctx context.Context, accountID, labelID uuid.UUID, name, color string) (Label, error) {
	if err := validateName(name); err != nil {
		return Label{}, err
	}
	if err := s.repo.Update(ctx, accountID, labelID, name, color); err != nil {
		return Label{}, err
	}
	return s.repo.ByID(ctx, accountID, labelID)
}

// Delete is logical, so the label stays reachable from the rows that were tagged with it before it went away (C09).
func (s *Service) Delete(ctx context.Context, accountID, labelID uuid.UUID) error {
	return s.repo.SoftDelete(ctx, accountID, labelID)
}

// Whitespace only passes the NOT NULL of the column and produces a label nobody can see in a list.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyName
	}
	return nil
}
