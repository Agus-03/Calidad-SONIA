package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

type Repository struct{ exec db.Executor }

func NewRepository(exec db.Executor) *Repository { return &Repository{exec: exec} }

// WithExecutor returns the same repository bound to a transaction, so the caller that conducts owns it and this one never opens it (regla 5).
func (r *Repository) WithExecutor(exec db.Executor) *Repository { return &Repository{exec: exec} }

// Reads four tables this package does not own: the ownership rule restricts writes and not reads, as turn/ already does (regla 1).
//
// The last message goes by created_at and not sent_at, because sent_at mixes Meta's clock and ours and only ours orders both
// directions on one line (C06, ADR-0036 regla 2). The id breaks the tie of a single transaction.
//
// Hidden messages are left out of both reads: hidden_at means "not shown in the inbox", and this is the inbox (C09). A contact taken
// down leaves the inbox with its conversations, which stay in the database as history (ADR-0035, regla 1).
//
// The failed status travels as $2 and is not written in, so a renamed value in enums.yaml cannot silently count zero (C07).
//
// The agent is joined for the state of the bot, with the same three conditions the turn and the switch read (ADR-0038).
const summarySelect = `
SELECT c.id, c.status, c.mode, c.ai_agent_id, COALESCE(a.active AND a.deleted_at IS NULL, false), c.starred, c.unread_count,
       c.quality_score, COALESCE(tags.items, '[]'::json),
       ct.id, ct.name, ct.lastname, ct.phone_whatsapp,
       op.id, op.name, op.lastname,
       latest.id, latest.direction, latest.sender_type, latest.content_text, latest.content_type,
       latest.status, latest.error_code, latest.sent_at,
       failed.failed_count, failed.last_failed_at,
       c.last_inbound_at, c.window_expires_at, c.last_activity_at
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
LEFT   JOIN ai_agents a ON a.id = c.ai_agent_id
LEFT   JOIN operators op ON op.id = c.operator_id
LEFT   JOIN LATERAL (
       SELECT m.id, m.direction, m.sender_type, m.content_text, m.content_type, m.status, m.error_code, m.sent_at
       FROM   messages m
       WHERE  m.conversation_id = c.id AND m.hidden_at IS NULL
       ORDER  BY m.created_at DESC, m.id DESC
       LIMIT  1
) latest ON true
LEFT   JOIN LATERAL (
       SELECT json_agg(json_build_object('id', l.id, 'name', l.name, 'color', l.color) ORDER BY l.name, l.id) AS items
       FROM   conversation_labels cl
       JOIN   labels l ON l.id = cl.label_id AND l.deleted_at IS NULL
       WHERE  cl.conversation_id = c.id
) tags ON true
CROSS  JOIN LATERAL (
       SELECT count(*) AS failed_count, max(f.created_at) AS last_failed_at
       FROM   messages f
       WHERE  f.conversation_id = c.id AND f.status = $2 AND f.hidden_at IS NULL
) failed
WHERE  c.deleted_at IS NULL AND ct.deleted_at IS NULL AND ct.account_id = $1`

// reach is built and not written as a constant because the narrowing adds an argument, and returning the args with the SQL is what
// keeps the placeholders from drifting apart.
//
// The operator sees what is assigned to it and the pool. Anexo B forbids it the conversations of other operators, and one in the
// pool is nobody's; hiding the pool would leave nobody able to answer first, which is how ADR-0004 assigns at all (ADR-0035, regla 1).
func reach(s Scope) (where string, args []any) {
	args = []any{s.AccountID, enums.MessageStatusFailed}
	if s.seesTheWholeTeam() {
		return "", args
	}
	args = append(args, s.OperatorID)
	return fmt.Sprintf(" AND (c.operator_id = $%d OR c.operator_id IS NULL)", len(args)), args
}

// narrow appends the filters of F23 after the reach, so a filter can only take rows away from what the role already sees (ADR-0050).
// Every value travels as a placeholder, the status too, so a renamed value in enums.yaml cannot silently match nothing (C07).
func narrow(where string, args []any, f Filter) (string, []any) {
	if f.Unread {
		where += " AND c.unread_count > 0"
	}
	if f.Assigned {
		where += " AND c.operator_id IS NOT NULL"
	}
	if f.Starred {
		where += " AND c.starred"
	}
	if f.Status != nil {
		args = append(args, *f.Status)
		where += fmt.Sprintf(" AND c.status = $%d", len(args))
	}
	// A label taken out of the catalog no longer exists for the inbox, like any row deleted logically (C09).
	if f.LabelID != nil {
		args = append(args, *f.LabelID)
		where += fmt.Sprintf(` AND EXISTS (
       SELECT 1 FROM conversation_labels cl JOIN labels l ON l.id = cl.label_id
       WHERE  cl.conversation_id = c.id AND cl.label_id = $%d AND l.deleted_at IS NULL)`, len(args))
	}
	// Reminders hang from the contact and not the conversation, so one raised in an earlier conversation still marks this one (ADR-0002 (c)).
	if f.WithReminder {
		where += ` AND EXISTS (
       SELECT 1 FROM reminders r
       WHERE  r.contact_id = c.contact_id AND r.deleted_at IS NULL)`
	}
	return where, args
}

// List answers up to limit rows after the cursor, newest activity first. Deciding whether another page exists is the service's.
//
// The row comparison is what makes the cursor exact: both columns descend together, so "(a, b) < (x, y)" is "strictly after the row
// the last page ended on", and the id settles two conversations touched in the same microsecond (ADR-0036, regla 3).
func (r *Repository) List(ctx context.Context, scope Scope, filter Filter, after *Cursor, limit int) ([]Summary, error) {
	where, args := reach(scope)
	where, args = narrow(where, args, filter)
	if after != nil {
		args = append(args, after.At, after.ID)
		where += fmt.Sprintf(" AND (c.last_activity_at, c.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit)
	listing := summarySelect + where + fmt.Sprintf(" ORDER BY c.last_activity_at DESC, c.id DESC LIMIT $%d", len(args))

	rows, err := r.exec.Query(ctx, listing, args...)
	if err != nil {
		return nil, fmt.Errorf("inbox: list conversations: %w", err)
	}
	defer rows.Close()

	out := []Summary{}
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbox: read conversations: %w", err)
	}
	return out, nil
}

// The account first and the row after, because summarySelect is scoped by account and the hub only knows the conversation: which
// operators may hear of it is decided after, per connection.
const summaryAccount = `
SELECT ct.account_id
FROM   conversations c
JOIN   contacts ct ON ct.id = c.contact_id
WHERE  c.id = $1 AND c.deleted_at IS NULL AND ct.deleted_at IS NULL`

// SummaryOf reads one row of the inbox as the listing draws it, for the event F21 pushes when it changes (ADR-0051).
func (r *Repository) SummaryOf(ctx context.Context, conversationID uuid.UUID) (uuid.UUID, Summary, error) {
	var accountID uuid.UUID
	err := r.exec.QueryRow(ctx, summaryAccount, conversationID).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, Summary{}, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, Summary{}, fmt.Errorf("inbox: read the account of a conversation: %w", err)
	}

	s, err := scanSummary(r.exec.QueryRow(ctx, summarySelect+" AND c.id = $3", accountID, enums.MessageStatusFailed, conversationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, Summary{}, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, Summary{}, err
	}
	return accountID, s, nil
}

// Scoped by the account of the contact, like the listing: a conversation of another account is one this caller cannot know exists,
// and one whose contact was taken down is one nobody answers any more (ADR-0035, regla 1).
// The agent is joined for the switch of the bot, which needs the three conditions the turn reads: it exists, is active and was not
// removed (ADR-0038). The ownership rule restricts writes and not reads.
const targetSelect = `
SELECT    c.id, c.inbox_id, ct.phone_whatsapp, c.operator_id, c.status, c.window_expires_at, c.ai_agent_id, c.mode,
          COALESCE(a.active AND a.deleted_at IS NULL, false), c.starred, c.unread_count
FROM      conversations c
JOIN      contacts ct ON ct.id = c.contact_id
LEFT JOIN ai_agents a ON a.id = c.ai_agent_id
WHERE     c.deleted_at IS NULL AND ct.deleted_at IS NULL AND ct.account_id = $1 AND c.id = $2`

// Target reads without the reach on purpose: whether this operator may send is a rule, and rules are the service's.
func (r *Repository) Target(ctx context.Context, accountID, conversationID uuid.UUID) (Target, error) {
	var t Target
	err := r.exec.QueryRow(ctx, targetSelect, accountID, conversationID).
		Scan(&t.ConversationID, &t.InboxID, &t.ContactPhone, &t.OperatorID, &t.Status, &t.WindowExpiresAt, &t.AIAgentID, &t.Mode,
			&t.AIAgentAvailable, &t.Starred, &t.UnreadCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("inbox: scan target: %w", err)
	}
	return t, nil
}

// The same two exclusions as the listing, for the same reasons: hidden messages are not shown in the inbox (C09), and the order is
// our clock and not the channel's (C06, ADR-0036 regla 2). The operator is joined so a thread a reassignment passed through says
// who wrote what.
const threadSelect = `
SELECT m.id, m.direction, m.sender_type, m.content_text, m.content_type, m.status, m.error_code, m.sent_at, m.created_at,
       op.id, op.name, op.lastname,
       (SELECT g.id FROM generations g WHERE g.message_id = m.id ORDER BY g.created_at LIMIT 1)
FROM   messages m
LEFT   JOIN operators op ON op.id = m.sender_operator_id
WHERE  m.conversation_id = $1 AND m.hidden_at IS NULL`

// Thread answers up to limit messages before the cursor, newest first. Whether the caller may read the conversation at all was
// decided before this is asked, which is why it takes no scope.
func (r *Repository) Thread(ctx context.Context, conversationID uuid.UUID, after *Cursor, limit int) ([]ThreadMessage, error) {
	args := []any{conversationID}
	where := ""
	if after != nil {
		args = append(args, after.At, after.ID)
		where = fmt.Sprintf(" AND (m.created_at, m.id) < ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, limit)
	query := threadSelect + where + fmt.Sprintf(" ORDER BY m.created_at DESC, m.id DESC LIMIT $%d", len(args))

	rows, err := r.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("inbox: read thread: %w", err)
	}
	defer rows.Close()

	out := []ThreadMessage{}
	for rows.Next() {
		m, err := scanThreadMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inbox: read thread: %w", err)
	}
	return out, nil
}

const assignmentInsert = `
INSERT INTO conversation_assignments (id, conversation_id, from_operator_id, to_operator_id, assigned_by_operator_id, reason)
VALUES ($1, $2, $3, $4, $5, nullif($6, ''))`

// RecordAssignment is only ever an INSERT: the table is the history F26 asks for, and history is not corrected (C09).
func (r *Repository) RecordAssignment(ctx context.Context, a Assignment) error {
	if _, err := r.exec.Exec(ctx, assignmentInsert,
		a.ID, a.ConversationID, a.FromOperatorID, a.ToOperatorID, a.AssignedByOperatorID, a.Reason); err != nil {
		return fmt.Errorf("inbox: record assignment: %w", err)
	}
	return nil
}

// The vendor of the contact's latest earlier conversation, if it still is one: a person who is active, not taken down and of the
// operator role (ADR-0037, reglas 1 y 2). An earlier conversation nobody held says nothing, so it is skipped; but the latest vendor
// being gone means the turn decides, and not a vendor from further back.
const previousOperatorSelect = `
SELECT o.id
FROM   (
       SELECT c.operator_id
       FROM   conversations c
       WHERE  c.contact_id = $1 AND c.id <> $2 AND c.deleted_at IS NULL AND c.operator_id IS NOT NULL
       ORDER  BY c.created_at DESC, c.id DESC
       LIMIT  1
) latest
JOIN   operators o ON o.id = latest.operator_id
WHERE  o.account_id = $3 AND o.role = $4 AND o.active AND o.deleted_at IS NULL`

// PreviousOperator answers nil when the contact has no earlier vendor who can still receive it.
func (r *Repository) PreviousOperator(ctx context.Context, accountID, contactID, conversationID uuid.UUID) (*uuid.UUID, error) {
	return r.oneOperator(ctx, "previous operator", previousOperatorSelect, contactID, conversationID, accountID, enums.OperatorRoleOperator)
}

// The vendor who has gone longest without receiving a conversation, and first of all one who never received any. Every assignment
// counts, whatever its reason: a returning customer or a conversation taken from the pool is work received too (ADR-0037, regla 1).
//
// The last assignment is read as a row and not as max(created_at): now() is the start of the transaction, so the assignments of one
// payload share an instant, and only their v7 id still says which came first. Without it, the lowest operator id would take every
// conversation of the payload after the first round.
const nextInTurnSelect = `
SELECT o.id
FROM   operators o
LEFT   JOIN LATERAL (
       SELECT a.created_at, a.id
       FROM   conversation_assignments a
       WHERE  a.to_operator_id = o.id
       ORDER  BY a.created_at DESC, a.id DESC
       LIMIT  1
) last ON true
WHERE  o.account_id = $1 AND o.role = $2 AND o.active AND o.deleted_at IS NULL
ORDER  BY last.created_at ASC NULLS FIRST, last.id ASC NULLS FIRST, o.id
LIMIT  1`

// NextInTurn answers nil when no vendor is active, which leaves the conversation in the pool (ADR-0037, regla 3).
func (r *Repository) NextInTurn(ctx context.Context, accountID uuid.UUID) (*uuid.UUID, error) {
	return r.oneOperator(ctx, "next operator in turn", nextInTurnSelect, accountID, enums.OperatorRoleOperator)
}

func (r *Repository) oneOperator(ctx context.Context, what, query string, args ...any) (*uuid.UUID, error) {
	var operatorID uuid.UUID
	err := r.exec.QueryRow(ctx, query, args...).Scan(&operatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inbox: read the %s: %w", what, err)
	}
	return &operatorID, nil
}

// The two LEFT JOINs arrive as nulls, so they are read into locals and moved to the model only when the group is there.
func scanSummary(row pgx.Row) (Summary, error) {
	var s Summary

	var operatorID *uuid.UUID
	var operatorName, operatorLastname *string

	var messageID *uuid.UUID
	var direction *enums.MessageDirection
	var sender *enums.SenderType
	var text *string
	var contentType *enums.MessageContentType
	var status *enums.MessageStatus
	var errorCode *string
	var sentAt *time.Time

	var tags []byte
	err := row.Scan(&s.ID, &s.Status, &s.Mode, &s.AIAgentID, &s.AIAgentAvailable, &s.Starred, &s.UnreadCount, &s.QualityScore, &tags,
		&s.Contact.ID, &s.Contact.Name, &s.Contact.Lastname, &s.Contact.PhoneWhatsapp,
		&operatorID, &operatorName, &operatorLastname,
		&messageID, &direction, &sender, &text, &contentType, &status, &errorCode, &sentAt,
		&s.FailedCount, &s.LastFailedAt,
		&s.LastInboundAt, &s.WindowExpiresAt, &s.LastActivityAt)
	if err != nil {
		return Summary{}, fmt.Errorf("inbox: scan conversation: %w", err)
	}
	if err := json.Unmarshal(tags, &s.Labels); err != nil {
		return Summary{}, fmt.Errorf("inbox: decode the labels of a conversation: %w", err)
	}

	if operatorID != nil && operatorName != nil && operatorLastname != nil {
		s.Operator = &Operator{ID: *operatorID, Name: *operatorName, Lastname: *operatorLastname}
	}
	if messageID != nil && direction != nil && sender != nil && text != nil && contentType != nil && sentAt != nil {
		s.LastMessage = &Message{
			ID: *messageID, Direction: *direction, SenderType: *sender, ContentText: *text, ContentType: *contentType,
			Status: status, ErrorCode: errorCode, SentAt: *sentAt,
		}
	}
	return s, nil
}

// The operator is a LEFT JOIN too: only an operator's answer has one, so it is moved to the model only when the group is there.
func scanThreadMessage(row pgx.Row) (ThreadMessage, error) {
	var m ThreadMessage
	var operatorID *uuid.UUID
	var operatorName, operatorLastname *string

	if err := row.Scan(&m.ID, &m.Direction, &m.SenderType, &m.ContentText, &m.ContentType, &m.Status, &m.ErrorCode, &m.SentAt,
		&m.CreatedAt, &operatorID, &operatorName, &operatorLastname, &m.GenerationID); err != nil {
		return ThreadMessage{}, fmt.Errorf("inbox: scan message: %w", err)
	}
	if operatorID != nil && operatorName != nil && operatorLastname != nil {
		m.SenderOperator = &Operator{ID: *operatorID, Name: *operatorName, Lastname: *operatorLastname}
	}
	return m, nil
}

// The newest draft still pending, read from generations, which the brain writes: the ownership rule restricts writes and not reads.
// An older pending one is a suggestion nobody used, and it stays as it is (ADR-0046, section 2.6).
const suggestionSelect = `
SELECT   g.id, g.completion_text, g.created_at
FROM     generations g
WHERE    g.conversation_id = $1 AND g.status = $2 AND g.message_id IS NULL
ORDER BY g.created_at DESC, g.id DESC
LIMIT    1`

// PendingSuggestion answers ErrNoSuggestion when there is no draft waiting, which is the normal state of most conversations.
func (r *Repository) PendingSuggestion(ctx context.Context, conversationID uuid.UUID) (Suggestion, error) {
	var s Suggestion
	err := r.exec.QueryRow(ctx, suggestionSelect, conversationID, enums.GenerationStatusPendingReview).
		Scan(&s.GenerationID, &s.Text, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Suggestion{}, ErrNoSuggestion
	}
	if err != nil {
		return Suggestion{}, fmt.Errorf("inbox: read suggestion: %w", err)
	}
	return s, nil
}

// The generation by its id and its conversation together, so an id of another conversation is not found rather than read.
const reasoningGeneration = `
SELECT g.id, g.status, g.llm_model, g.created_at
FROM   generations g
WHERE  g.id = $1 AND g.conversation_id = $2`

// No filter on deleted_at: the version is what was assembled, and removing the component later does not unsay it (C09, ADR-0039).
const reasoningComponents = `
SELECT   pc.type, gc.position, v.version_number, v.content
FROM     generation_components gc
JOIN     prompt_component_versions v ON v.id = gc.prompt_component_version_id
JOIN     prompt_components pc ON pc.id = v.prompt_component_id
WHERE    gc.generation_id = $1
ORDER BY gc.position`

// Reasoning answers ErrGenerationNotFound when the generation is not one of this conversation. It reads the text of every version,
// and the service is the one that decides who keeps it.
func (r *Repository) Reasoning(ctx context.Context, conversationID, generationID uuid.UUID) (Reasoning, error) {
	var out Reasoning
	err := r.exec.QueryRow(ctx, reasoningGeneration, generationID, conversationID).
		Scan(&out.GenerationID, &out.Status, &out.LLMModel, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reasoning{}, ErrGenerationNotFound
	}
	if err != nil {
		return Reasoning{}, fmt.Errorf("inbox: read generation: %w", err)
	}

	rows, err := r.exec.Query(ctx, reasoningComponents, generationID)
	if err != nil {
		return Reasoning{}, fmt.Errorf("inbox: read generation components: %w", err)
	}
	defer rows.Close()

	out.Components = []UsedComponent{}
	for rows.Next() {
		var c UsedComponent
		var content string
		if err := rows.Scan(&c.Type, &c.Position, &c.VersionNumber, &content); err != nil {
			return Reasoning{}, fmt.Errorf("inbox: scan generation component: %w", err)
		}
		c.Content = &content
		out.Components = append(out.Components, c)
	}
	if err := rows.Err(); err != nil {
		return Reasoning{}, fmt.Errorf("inbox: read generation components: %w", err)
	}
	return out, nil
}

// Whether the person can take conversations now, read in the caller's account: one of another account is one that does not exist.
const operatorState = `
SELECT o.active AND o.deleted_at IS NULL
FROM   operators o
WHERE  o.id = $1 AND o.account_id = $2`

func (r *Repository) OperatorState(ctx context.Context, accountID, operatorID uuid.UUID) (bool, error) {
	var available bool
	err := r.exec.QueryRow(ctx, operatorState, operatorID, accountID).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrOperatorNotFound
	}
	if err != nil {
		return false, fmt.Errorf("inbox: read the operator: %w", err)
	}
	return available, nil
}

// Oldest first, which is how a history is read; the id breaks the tie of rows written in one transaction, like a handover's.
const assignmentsSelect = `
SELECT a.id, COALESCE(a.reason, ''), a.created_at,
       f.id, f.name, f.lastname,
       t.id, t.name, t.lastname,
       b.id, b.name, b.lastname
FROM   conversation_assignments a
JOIN   operators t ON t.id = a.to_operator_id
LEFT   JOIN operators f ON f.id = a.from_operator_id
LEFT   JOIN operators b ON b.id = a.assigned_by_operator_id
WHERE  a.conversation_id = $1
ORDER  BY a.created_at, a.id`

func (r *Repository) Assignments(ctx context.Context, conversationID uuid.UUID) ([]AssignmentEntry, error) {
	rows, err := r.exec.Query(ctx, assignmentsSelect, conversationID)
	if err != nil {
		return nil, fmt.Errorf("inbox: read the assignments: %w", err)
	}
	defer rows.Close()

	out := []AssignmentEntry{}
	for rows.Next() {
		var e AssignmentEntry
		var fromID, byID *uuid.UUID
		var fromName, fromLastname, byName, byLastname *string
		if err := rows.Scan(&e.ID, &e.Reason, &e.CreatedAt, &fromID, &fromName, &fromLastname,
			&e.To.ID, &e.To.Name, &e.To.Lastname, &byID, &byName, &byLastname); err != nil {
			return nil, fmt.Errorf("inbox: scan an assignment: %w", err)
		}
		e.From = named(fromID, fromName, fromLastname)
		e.AssignedBy = named(byID, byName, byLastname)
		out = append(out, e)
	}
	return out, rows.Err()
}

// named is the person of a LEFT JOIN, which arrives as three nulls when there is none.
func named(operatorID *uuid.UUID, name, lastname *string) *Operator {
	if operatorID == nil || name == nil || lastname == nil {
		return nil
	}
	return &Operator{ID: *operatorID, Name: *name, Lastname: *lastname}
}

// A deleted note leaves the thread and keeps its row (C09); the author is joined so the note is read with its signature.
const noteSelect = `
SELECT n.id, n.conversation_id, o.id, o.name, o.lastname, n.content, n.created_at, n.updated_at
FROM   internal_notes n
JOIN   operators o ON o.id = n.operator_id
WHERE  n.deleted_at IS NULL`

func (r *Repository) Notes(ctx context.Context, conversationID uuid.UUID) ([]Note, error) {
	rows, err := r.exec.Query(ctx, noteSelect+" AND n.conversation_id = $1 ORDER BY n.created_at, n.id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("inbox: read the notes: %w", err)
	}
	defer rows.Close()

	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NoteByID reads the note only within its conversation, so an id of another thread is a note that does not exist.
func (r *Repository) NoteByID(ctx context.Context, conversationID, noteID uuid.UUID) (Note, error) {
	n, err := scanNote(r.exec.QueryRow(ctx, noteSelect+" AND n.conversation_id = $1 AND n.id = $2", conversationID, noteID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNoteNotFound
	}
	return n, err
}

func scanNote(row pgx.Row) (Note, error) {
	var n Note
	err := row.Scan(&n.ID, &n.ConversationID, &n.Author.ID, &n.Author.Name, &n.Author.Lastname, &n.Content, &n.CreatedAt, &n.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Note{}, fmt.Errorf("inbox: scan a note: %w", err)
	}
	return n, err
}

func (r *Repository) AddNote(ctx context.Context, n Note) error {
	if _, err := r.exec.Exec(ctx, `INSERT INTO internal_notes (id, conversation_id, operator_id, content) VALUES ($1, $2, $3, $4)`,
		n.ID, n.ConversationID, n.Author.ID, n.Content); err != nil {
		return fmt.Errorf("inbox: add a note: %w", err)
	}
	return nil
}

// The author is in the predicate of both writes, so the rule holds even if the read before it was stale (ADR-0056).
func (r *Repository) EditNote(ctx context.Context, noteID, operatorID uuid.UUID, content string) error {
	return r.noteWrite(ctx, "edit", `UPDATE internal_notes SET content = $3, updated_at = now()
		WHERE id = $1 AND operator_id = $2 AND deleted_at IS NULL`, noteID, operatorID, content)
}

func (r *Repository) DeleteNote(ctx context.Context, noteID, operatorID uuid.UUID) error {
	return r.noteWrite(ctx, "delete", `UPDATE internal_notes SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND operator_id = $2 AND deleted_at IS NULL`, noteID, operatorID)
}

func (r *Repository) noteWrite(ctx context.Context, what, query string, args ...any) error {
	tag, err := r.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("inbox: %s a note: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoteNotFound
	}
	return nil
}

// ON CONFLICT DO NOTHING and not a refusal: tagging twice is the same request twice, and the row already says what was asked (ADR-0057).
const conversationLabelInsert = `
INSERT INTO conversation_labels (id, conversation_id, label_id, assigned_by_operator_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id, label_id) DO NOTHING`

// AssignLabel answers whether a link was written, so only a real change is told to the hub.
func (r *Repository) AssignLabel(ctx context.Context, linkID, conversationID, labelID, operatorID uuid.UUID) (bool, error) {
	tag, err := r.exec.Exec(ctx, conversationLabelInsert, linkID, conversationID, labelID, operatorID)
	if err != nil {
		return false, fmt.Errorf("inbox: assign label: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// A DELETE, the exception to C09 the schema declares for this table: unlabelling is routine and no metric counts it.
func (r *Repository) UnassignLabel(ctx context.Context, conversationID, labelID uuid.UUID) (bool, error) {
	tag, err := r.exec.Exec(ctx, `DELETE FROM conversation_labels WHERE conversation_id = $1 AND label_id = $2`, conversationID, labelID)
	if err != nil {
		return false, fmt.Errorf("inbox: unassign label: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// The same read as the row of the listing, so the answer of a tag and the event that follows it cannot disagree (ADR-0057).
const conversationLabelsSelect = `
SELECT l.id, l.name, l.color
FROM   conversation_labels cl
JOIN   labels l ON l.id = cl.label_id AND l.deleted_at IS NULL
WHERE  cl.conversation_id = $1
ORDER  BY l.name, l.id`

func (r *Repository) LabelsOf(ctx context.Context, conversationID uuid.UUID) ([]Label, error) {
	rows, err := r.exec.Query(ctx, conversationLabelsSelect, conversationID)
	if err != nil {
		return nil, fmt.Errorf("inbox: read the labels: %w", err)
	}
	defer rows.Close()

	out := []Label{}
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Color); err != nil {
			return nil, fmt.Errorf("inbox: scan a label: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
