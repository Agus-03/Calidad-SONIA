package operators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// Eight characters, the floor NIST SP 800-63B sets for a password a person chose; the ceiling is bcrypt's 72 bytes (ADR-0030).
const minPasswordLength = 8

var (
	ErrEmptyName       = errors.New("operators: the name or the lastname is empty")
	ErrInvalidEmail    = errors.New("operators: the email is not an address")
	ErrInvalidRole     = errors.New("operators: the role is not one of the three")
	ErrPasswordTooWeak = errors.New("operators: the password is shorter than eight characters")

	// ErrSelf refuses what would lock the admin out: their own role, active, removal or a reset that skips the current password.
	ErrSelf = errors.New("operators: an admin cannot do this to themselves")

	// ErrLastAdmin keeps the account from being left with nobody able to manage it (ADR-0062).
	ErrLastAdmin = errors.New("operators: the account would be left without an active admin")

	// ErrWrongCurrentPassword is told plainly, because the caller already holds a token and nothing is enumerated by it.
	ErrWrongCurrentPassword = errors.New("operators: the current password does not match")
)

// The audit's verbs, the same stable words agents/ writes, so one query filters every entity alike (ADR-0007).
const (
	auditCreate = "create"
	auditUpdate = "update"
	auditDelete = "delete"
)

// Auditor is agents/: audit_logs is its table, so the roster writes through it and never with SQL of its own (regla 1).
type Auditor interface {
	RecordOperatorChange(ctx context.Context, actorID, operatorID uuid.UUID, action string, changes []byte) error
}

// RosterStore is what the roster writes inside the transaction it conducts, bound from internal/app/deps.go (regla 5).
type RosterStore interface {
	LockAdmins(ctx context.Context, accountID uuid.UUID) (int, error)
	ForUpdate(ctx context.Context, accountID, operatorID uuid.UUID) (Operator, error)
	Register(ctx context.Context, o Operator) error
	Update(ctx context.Context, o Operator) error
	SetPassword(ctx context.Context, accountID, operatorID uuid.UUID, hash, replaced string) error
	SoftDelete(ctx context.Context, accountID, operatorID uuid.UUID) error
	HeldConversations(ctx context.Context, operatorID uuid.UUID) (int, error)
}

// RosterTxFunc runs fn in one transaction with both collaborators bound to it: the change and its audit commit together.
type RosterTxFunc func(ctx context.Context, fn func(RosterStore, Auditor) error) error

type rosterReader interface {
	List(ctx context.Context, accountID uuid.UUID, f Filter, limit, offset int) (Page, error)
	ByID(ctx context.Context, operatorID uuid.UUID) (Operator, error)
}

type RosterOptions struct {
	Repo   rosterReader
	Tx     RosterTxFunc
	Logger *slog.Logger
}

// Roster is F18 apart from the login: the admin manages the team and everybody changes their own password (ADR-0062).
type Roster struct{ opts RosterOptions }

func NewRoster(opts RosterOptions) *Roster {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Roster{opts: opts}
}

// Scope is who asks, from the token: the account bounds every read and the operator is who the self guards compare against.
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
}

// NewOperator is what the admin types; the password travels in clear only as far as Hash (ADR-0030).
type NewOperator struct {
	Name, Lastname, Email, Password string
	Role                            enums.OperatorRole
}

// Patch carries pointers so a field left out is told apart from one sent empty: deactivating sends only active.
type Patch struct {
	Name, Lastname, Email *string
	Role                  *enums.OperatorRole
	Active                *bool
}

// Held is the answer of a change that can leave conversations behind: how many the person still has, to hand over (ADR-0062).
type Held struct {
	Operator      Operator
	Conversations int
}

func (r *Roster) List(ctx context.Context, scope Scope, f Filter, limit, offset int) (Page, error) {
	return r.opts.Repo.List(ctx, scope.AccountID, f, limit, offset)
}

func (r *Roster) Create(ctx context.Context, scope Scope, in NewOperator) (Operator, error) {
	o := Operator{
		ID: id.New(), AccountID: scope.AccountID, Name: strings.TrimSpace(in.Name), Lastname: strings.TrimSpace(in.Lastname),
		Email: normalizeEmail(in.Email), Role: in.Role, Active: true,
	}
	if err := validate(o); err != nil {
		return Operator{}, err
	}
	// Hashed before the transaction, so bcrypt's quarter second is not spent holding it open.
	hash, err := hashChecked(in.Password)
	if err != nil {
		return Operator{}, err
	}
	o.PasswordHash = hash

	err = r.opts.Tx(ctx, func(store RosterStore, audit Auditor) error {
		if err := store.Register(ctx, o); err != nil {
			return err
		}
		return record(ctx, audit, scope.OperatorID, o.ID, auditCreate, change{After: audited(o)})
	})
	if err != nil {
		return Operator{}, err
	}
	r.opts.Logger.Info("operator created", slog.String("operator_id", o.ID.String()), slog.String("by", scope.OperatorID.String()))
	return o, nil
}

// Update merges the patch over the locked row; a save that changes nothing writes nothing, audit included (ADR-0038, regla 9).
func (r *Roster) Update(ctx context.Context, scope Scope, operatorID uuid.UUID, p Patch) (Held, error) {
	var out Held
	err := r.opts.Tx(ctx, func(store RosterStore, audit Auditor) error {
		admins := 0
		if p.Role != nil || p.Active != nil {
			var err error
			if admins, err = store.LockAdmins(ctx, scope.AccountID); err != nil {
				return err
			}
		}
		before, err := store.ForUpdate(ctx, scope.AccountID, operatorID)
		if err != nil {
			return err
		}
		after := merged(before, p)
		if err := validate(after); err != nil {
			return err
		}
		// Only a real change counts, so a form that sends the whole row back with the same role still saves the admin's name.
		if operatorID == scope.OperatorID && (after.Role != before.Role || after.Active != before.Active) {
			return ErrSelf
		}
		if lastAdmin(before, after.Role == enums.OperatorRoleAdmin && after.Active, admins) {
			return ErrLastAdmin
		}
		out.Operator = after
		if view(before) != view(after) {
			if err := store.Update(ctx, after); err != nil {
				return err
			}
			if err := record(ctx, audit, scope.OperatorID, operatorID, auditUpdate, change{Before: audited(before), After: audited(after)}); err != nil {
				return err
			}
		}
		out.Conversations, err = store.HeldConversations(ctx, operatorID)
		return err
	})
	if err != nil {
		return Held{}, err
	}
	return out, nil
}

// Delete is logical and frees the address; the conversations stay with the person until the admin hands them over (ADR-0062).
func (r *Roster) Delete(ctx context.Context, scope Scope, operatorID uuid.UUID) (int, error) {
	if operatorID == scope.OperatorID {
		return 0, ErrSelf
	}
	held := 0
	err := r.opts.Tx(ctx, func(store RosterStore, audit Auditor) error {
		admins, err := store.LockAdmins(ctx, scope.AccountID)
		if err != nil {
			return err
		}
		before, err := store.ForUpdate(ctx, scope.AccountID, operatorID)
		if err != nil {
			return err
		}
		if lastAdmin(before, false, admins) {
			return ErrLastAdmin
		}
		if err := store.SoftDelete(ctx, scope.AccountID, operatorID); err != nil {
			return err
		}
		if err := record(ctx, audit, scope.OperatorID, operatorID, auditDelete, change{Before: audited(before)}); err != nil {
			return err
		}
		held, err = store.HeldConversations(ctx, operatorID)
		return err
	})
	if err != nil {
		return 0, err
	}
	r.opts.Logger.Info("operator deleted", slog.String("operator_id", operatorID.String()), slog.String("by", scope.OperatorID.String()))
	return held, nil
}

// ResetPassword is the way back for whoever forgot theirs: the admin sets a new one and tells them (ADR-0062).
func (r *Roster) ResetPassword(ctx context.Context, scope Scope, operatorID uuid.UUID, password string) error {
	// Their own goes through ChangePassword, which asks for the current one: a stolen session must not be enough.
	if operatorID == scope.OperatorID {
		return ErrSelf
	}
	hash, err := hashChecked(password)
	if err != nil {
		return err
	}
	return r.opts.Tx(ctx, func(store RosterStore, audit Auditor) error {
		target, err := store.ForUpdate(ctx, scope.AccountID, operatorID)
		if err != nil {
			return err
		}
		if err := store.SetPassword(ctx, scope.AccountID, operatorID, hash, target.PasswordHash); err != nil {
			return err
		}
		return record(ctx, audit, scope.OperatorID, operatorID, auditUpdate, change{Password: "reset"})
	})
}

// ChangePassword is anyone's own, proved with the current one; both bcrypt runs happen before the transaction opens.
func (r *Roster) ChangePassword(ctx context.Context, scope Scope, current, next string) error {
	me, err := r.opts.Repo.ByID(ctx, scope.OperatorID)
	if err != nil {
		return err
	}
	if err := Verify(me.PasswordHash, current); err != nil {
		if errors.Is(err, ErrWrongPassword) {
			return ErrWrongCurrentPassword
		}
		return err
	}
	hash, err := hashChecked(next)
	if err != nil {
		return err
	}
	return r.opts.Tx(ctx, func(store RosterStore, audit Auditor) error {
		// The hash just verified is the predicate, so a reset that landed in between is not undone by this change.
		if err := store.SetPassword(ctx, scope.AccountID, scope.OperatorID, hash, me.PasswordHash); err != nil {
			return err
		}
		return record(ctx, audit, scope.OperatorID, scope.OperatorID, auditUpdate, change{Password: "changed"})
	})
}

// lastAdmin is true when the row is an active admin, stays one no longer, and is the only one the lock found.
func lastAdmin(before Operator, stillAdmin bool, admins int) bool {
	wasAdmin := before.Role == enums.OperatorRoleAdmin && before.Active
	return wasAdmin && !stillAdmin && admins <= 1
}

func merged(o Operator, p Patch) Operator {
	if p.Name != nil {
		o.Name = strings.TrimSpace(*p.Name)
	}
	if p.Lastname != nil {
		o.Lastname = strings.TrimSpace(*p.Lastname)
	}
	if p.Email != nil {
		o.Email = normalizeEmail(*p.Email)
	}
	if p.Role != nil {
		o.Role = *p.Role
	}
	if p.Active != nil {
		o.Active = *p.Active
	}
	return o
}

func validate(o Operator) error {
	if o.Name == "" || o.Lastname == "" {
		return ErrEmptyName
	}
	// The bare address and nothing else: "Ana <ana@x.com>" parses, and it is not what the login is typed with.
	if parsed, err := mail.ParseAddress(o.Email); err != nil || parsed.Address != o.Email {
		return ErrInvalidEmail
	}
	if !o.Role.IsValid() {
		return ErrInvalidRole
	}
	return nil
}

// Counted in characters and not bytes, which is what a person counts; the byte ceiling is left to Hash (ADR-0030).
func hashChecked(password string) (string, error) {
	if utf8.RuneCountInString(password) < minPasswordLength {
		return "", ErrPasswordTooWeak
	}
	return Hash(password)
}

// publicView is what the audit keeps of a person: never the hash, which would turn audit_logs into a copy of the passwords.
type publicView struct {
	Name     string             `json:"name"`
	Lastname string             `json:"lastname"`
	Email    string             `json:"email"`
	Role     enums.OperatorRole `json:"role"`
	Active   bool               `json:"active"`
}

func view(o Operator) publicView {
	return publicView{Name: o.Name, Lastname: o.Lastname, Email: o.Email, Role: o.Role, Active: o.Active}
}

func audited(o Operator) *publicView {
	v := view(o)
	return &v
}

// The before and after shape of ADR-0038; a password change says only that it happened and how, never what it became.
type change struct {
	Before   *publicView `json:"before,omitempty"`
	After    *publicView `json:"after,omitempty"`
	Password string      `json:"password,omitempty"`
}

func record(ctx context.Context, audit Auditor, actorID, operatorID uuid.UUID, action string, ch change) error {
	changes, err := json.Marshal(ch)
	if err != nil {
		return fmt.Errorf("operators: encode the audit of %s: %w", action, err)
	}
	return audit.RecordOperatorChange(ctx, actorID, operatorID, action, changes)
}
