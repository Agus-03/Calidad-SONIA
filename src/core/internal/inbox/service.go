package inbox

import (
	"context"
	"log/slog"
)

// store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type store interface {
	List(ctx context.Context, scope Scope, after *Cursor, limit int) ([]Summary, error)
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

// List asks for one row more than it answers, because that row is the only honest way to know another page exists: a full page could
// just as well be the last one, and a cursor handed out for nothing sends the frontend to ask for an empty page.
func (s *Service) List(ctx context.Context, scope Scope, after *Cursor, limit int) (Page, error) {
	// The controller never sends less than one, but a page of zero would index the row before the first one below.
	if limit < 1 {
		limit = 1
	}

	rows, err := s.repo.List(ctx, scope, after, limit+1)
	if err != nil {
		return Page{}, err
	}

	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := page.Items[limit-1]
		page.Next = &Cursor{LastActivityAt: last.LastActivityAt, ID: last.ID}
	}
	return page, nil
}
