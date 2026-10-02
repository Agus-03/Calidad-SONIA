package quickreplies

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
)

// Postgres says 23505 when a unique index rejects the row, which is how a title taken in another casing announces itself.
const uniqueViolation = "23505"

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Deleted rows are excluded here and nowhere else, matching the partial unique index that frees a title on delete (C09).
const replySelect = `
SELECT id, account_id, title, content, active, created_by_operator_id
FROM   quick_replies
WHERE  deleted_at IS NULL`

// strpos and not LIKE, so a % or an _ the operator types is searched as itself; an empty text matches every reply.
const replyFilter = `
  AND account_id = $1
  AND ($2::text = '' OR strpos(lower(title), lower($2::text)) > 0 OR strpos(lower(content), lower($2::text)) > 0)
  AND (active OR NOT $3::boolean)`

// Ordered by lower(title) so the listing agrees with the index that decides which titles collide.
const replyList = replySelect + replyFilter + `
ORDER BY lower(title)
LIMIT    $4 OFFSET $5`

const replyCount = `
SELECT count(*)
FROM   quick_replies
WHERE  deleted_at IS NULL` + replyFilter

// List returns the page and the total in two queries, as labels/ does, because a window count repeats the total on every row.
func (r *Repository) List(ctx context.Context, accountID uuid.UUID, text string, onlyActive bool, limit, offset int) (Page, error) {
	rows, err := r.exec.Query(ctx, replyList, accountID, text, onlyActive, limit, offset)
	if err != nil {
		return Page{}, fmt.Errorf("quick_replies: list quick replies: %w", err)
	}
	defer rows.Close()

	page := Page{Items: []QuickReply{}}
	for rows.Next() {
		q, err := scan(rows)
		if err != nil {
			return Page{}, err
		}
		page.Items = append(page.Items, q)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("quick_replies: read quick replies: %w", err)
	}

	if err := r.exec.QueryRow(ctx, replyCount, accountID, text, onlyActive).Scan(&page.Total); err != nil {
		return Page{}, fmt.Errorf("quick_replies: count quick replies: %w", err)
	}
	return page, nil
}

// ByID is scoped by account and not only by id, so an id guessed from another account reads as missing rather than as forbidden.
func (r *Repository) ByID(ctx context.Context, accountID, replyID uuid.UUID) (QuickReply, error) {
	q, err := scan(r.exec.QueryRow(ctx, replySelect+" AND account_id = $1 AND id = $2", accountID, replyID))
	if errors.Is(err, pgx.ErrNoRows) {
		return QuickReply{}, ErrNotFound
	}
	return q, err
}

// ON CONFLICT and not a rescued error, so the insert never aborts a transaction around it (SQLSTATE 25P02), as in labels/.
const replyInsert = `
INSERT INTO quick_replies (id, account_id, title, content, created_by_operator_id, active)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (account_id, lower(title)) WHERE deleted_at IS NULL DO NOTHING`

func (r *Repository) Create(ctx context.Context, q QuickReply) error {
	tag, err := r.exec.Exec(ctx, replyInsert, q.ID, q.AccountID, normalizeTitle(q.Title), q.Content, q.CreatedByOperatorID, q.Active)
	if err != nil {
		return fmt.Errorf("quick_replies: insert quick reply: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateTitle
	}
	return nil
}

// COALESCE keeps what the PATCH left out, so pausing a reply sends only active (ADR-0060).
const replyUpdate = `
UPDATE quick_replies
SET    title = COALESCE($3, title), content = COALESCE($4, content), active = COALESCE($5, active), updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// Update rescues the violation instead of avoiding it, because UPDATE has no ON CONFLICT and renaming onto a taken title is the case.
func (r *Repository) Update(ctx context.Context, accountID, replyID uuid.UUID, title, content *string, active *bool) error {
	if title != nil {
		trimmed := normalizeTitle(*title)
		title = &trimmed
	}
	tag, err := r.exec.Exec(ctx, replyUpdate, accountID, replyID, title, content, active)
	if isUniqueViolation(err) {
		return ErrDuplicateTitle
	}
	if err != nil {
		return fmt.Errorf("quick_replies: update quick reply: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// The predicate repeats deleted_at IS NULL so deleting twice reports a missing reply instead of moving the timestamp.
const replySoftDelete = `
UPDATE quick_replies
SET    deleted_at = now(), updated_at = now()
WHERE  account_id = $1 AND id = $2 AND deleted_at IS NULL`

// SoftDelete and not a DELETE, because the catalogue keeps its rows like every other table (C09).
func (r *Repository) SoftDelete(ctx context.Context, accountID, replyID uuid.UUID) error {
	tag, err := r.exec.Exec(ctx, replySoftDelete, accountID, replyID)
	if err != nil {
		return fmt.Errorf("quick_replies: delete quick reply: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scan(row pgx.Row) (QuickReply, error) {
	var q QuickReply
	if err := row.Scan(&q.ID, &q.AccountID, &q.Title, &q.Content, &q.Active, &q.CreatedByOperatorID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return QuickReply{}, err
		}
		return QuickReply{}, fmt.Errorf("quick_replies: scan quick reply: %w", err)
	}
	return q, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
