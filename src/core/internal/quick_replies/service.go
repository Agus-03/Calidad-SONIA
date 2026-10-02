package quickreplies

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	List(ctx context.Context, accountID uuid.UUID, text string, onlyActive bool, limit, offset int) (Page, error)
	ByID(ctx context.Context, accountID, replyID uuid.UUID) (QuickReply, error)
	Create(ctx context.Context, q QuickReply) error
	Update(ctx context.Context, accountID, replyID uuid.UUID, title, content *string, active *bool) error
	SoftDelete(ctx context.Context, accountID, replyID uuid.UUID) error
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

// List hides the paused replies from everyone but the admin, who is the one who pauses them and has to find them again (ADR-0060).
func (s *Service) List(ctx context.Context, scope Scope, text string, limit, offset int) (Page, error) {
	return s.repo.List(ctx, scope.AccountID, strings.TrimSpace(text), scope.Role != enums.OperatorRoleAdmin, limit, offset)
}

// Create returns the row it wrote so the response carries the id the caller did not choose (C04).
func (s *Service) Create(ctx context.Context, scope Scope, title, content string) (QuickReply, error) {
	if err := validate(&title, &content); err != nil {
		return QuickReply{}, err
	}
	reply := QuickReply{
		ID: id.New(), AccountID: scope.AccountID, Title: normalizeTitle(title), Content: content,
		Active: true, CreatedByOperatorID: scope.OperatorID,
	}
	if err := s.repo.Create(ctx, reply); err != nil {
		return QuickReply{}, err
	}
	s.log.Info("quick reply created", slog.String("quick_reply_id", reply.ID.String()), slog.String("operator_id", scope.OperatorID.String()))
	return reply, nil
}

// Update reads the row back because the repository reports that it changed and not what the row now says.
func (s *Service) Update(ctx context.Context, scope Scope, replyID uuid.UUID, title, content *string, active *bool) (QuickReply, error) {
	if err := validate(title, content); err != nil {
		return QuickReply{}, err
	}
	if err := s.repo.Update(ctx, scope.AccountID, replyID, title, content, active); err != nil {
		return QuickReply{}, err
	}
	return s.repo.ByID(ctx, scope.AccountID, replyID)
}

func (s *Service) Delete(ctx context.Context, scope Scope, replyID uuid.UUID) error {
	return s.repo.SoftDelete(ctx, scope.AccountID, replyID)
}

// A nil field is one the PATCH left out and passes; a blank one would be a reply nobody can pick or send.
func validate(title, content *string) error {
	if title != nil && strings.TrimSpace(*title) == "" {
		return ErrEmptyTitle
	}
	if content != nil && strings.TrimSpace(*content) == "" {
		return ErrEmptyContent
	}
	return nil
}
