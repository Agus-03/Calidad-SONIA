package sales

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// defaultCurrency is the schema's DEFAULT, said here too so a sale with no currency is not one the service invents.
const defaultCurrency = "ARS"

var (
	// numeric(14,2): twelve digits before the point and at most two after it (C08).
	amountPattern   = regexp.MustCompile(`^\d{1,12}(\.\d{1,2})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type store interface {
	Conversation(ctx context.Context, accountID, conversationID uuid.UUID) (Conversation, error)
	ByID(ctx context.Context, accountID, saleID uuid.UUID) (Sale, error)
	OfConversation(ctx context.Context, accountID, conversationID uuid.UUID) ([]Sale, error)
	Create(ctx context.Context, s Sale) error
	Update(ctx context.Context, saleID uuid.UUID, amount, currency string) error
	Delete(ctx context.Context, saleID uuid.UUID) error
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

// Mark is F14: the vendor of the conversation marks it, and so do supervisor and admin, and the sale is always the vendor's (ADR-0059).
func (s *Service) Mark(ctx context.Context, scope Scope, conversationID uuid.UUID, amount, currency string) (Sale, error) {
	amount, currency, err := valid(amount, currency)
	if err != nil {
		return Sale{}, err
	}
	c, err := s.reached(ctx, scope, conversationID)
	if err != nil {
		return Sale{}, err
	}
	if c.OperatorID == nil {
		return Sale{}, ErrNotAssigned
	}

	// The agent is copied as it is now, so the sale keeps who handled it after the conversation changes agent (F14, ADR-0059).
	sale := Sale{ID: id.New(), ConversationID: conversationID, OperatorID: *c.OperatorID, AIAgentID: c.AIAgentID, Amount: amount, Currency: currency}
	if err := s.repo.Create(ctx, sale); err != nil {
		return Sale{}, err
	}
	return s.repo.ByID(ctx, scope.AccountID, sale.ID)
}

// OfConversation has the reach of the thread: the operator reads the sales of its own and of the pool, the roles above all of them.
func (s *Service) OfConversation(ctx context.Context, scope Scope, conversationID uuid.UUID) ([]Sale, error) {
	if _, err := s.reached(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	return s.repo.OfConversation(ctx, scope.AccountID, conversationID)
}

// Update corrects the amount or the currency; who is credited and with which agent were fixed when it was marked (ADR-0059).
func (s *Service) Update(ctx context.Context, scope Scope, saleID uuid.UUID, amount, currency *string) (Sale, error) {
	current, err := s.changeable(ctx, scope, saleID)
	if err != nil {
		return Sale{}, err
	}
	nextAmount, nextCurrency := current.Amount, current.Currency
	if amount != nil {
		nextAmount = *amount
	}
	if currency != nil {
		nextCurrency = *currency
	}
	if nextAmount, nextCurrency, err = valid(nextAmount, nextCurrency); err != nil {
		return Sale{}, err
	}
	if err := s.repo.Update(ctx, saleID, nextAmount, nextCurrency); err != nil {
		return Sale{}, err
	}
	return s.repo.ByID(ctx, scope.AccountID, saleID)
}

func (s *Service) Delete(ctx context.Context, scope Scope, saleID uuid.UUID) error {
	if _, err := s.changeable(ctx, scope, saleID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, saleID)
}

// reached is the reach of the thread: an operator reaches its own and the pool, and another's does not exist for it (ADR-0035).
func (s *Service) reached(ctx context.Context, scope Scope, conversationID uuid.UUID) (Conversation, error) {
	c, err := s.repo.Conversation(ctx, scope.AccountID, conversationID)
	if err != nil {
		return Conversation{}, err
	}
	if !scope.seesTheWholeTeam() && c.OperatorID != nil && *c.OperatorID != scope.OperatorID {
		return Conversation{}, ErrConversationNotFound
	}
	return c, nil
}

// changeable lets through the vendor credited and the two roles above it (ADR-0059).
func (s *Service) changeable(ctx context.Context, scope Scope, saleID uuid.UUID) (Sale, error) {
	sale, err := s.repo.ByID(ctx, scope.AccountID, saleID)
	if err != nil {
		return Sale{}, err
	}
	if sale.OperatorID != scope.OperatorID && !scope.seesTheWholeTeam() {
		return Sale{}, ErrNotTheVendor
	}
	return sale, nil
}

// valid refuses an amount of zero too: a conversion with no money is not a sale F35 can add (C08).
func valid(amount, currency string) (string, string, error) {
	amount = strings.TrimSpace(amount)
	if !amountPattern.MatchString(amount) || strings.Trim(amount, "0.") == "" {
		return "", "", ErrInvalidAmount
	}
	currency = strings.TrimSpace(currency)
	if currency == "" {
		currency = defaultCurrency
	}
	if !currencyPattern.MatchString(currency) {
		return "", "", ErrInvalidCurrency
	}
	return amount, currency, nil
}
