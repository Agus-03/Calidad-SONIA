package reminders

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// How late an alert may arrive: F28 dates a reminder, it does not time it to the second (ADR-0058).
	defaultInterval = 30 * time.Second
	// At most this many per tick, so a core that was down drains its backlog over several ticks, as the turn's poller does (ADR-0041).
	defaultLimit = 100
)

// Alerts is the hub as this package sees it: one person told, on every socket it has open (ADR-0051, ADR-0058).
type Alerts interface {
	Remind(recipient uuid.UUID, reminder any)
}

type dueStore interface {
	Due(ctx context.Context, limit int) ([]Due, error)
}

type PollerOptions struct {
	Store  dueStore
	Alerts Alerts
	// Zero takes the defaults above, which is what the core runs with.
	Interval time.Duration
	Limit    int
	Logger   *slog.Logger
}

// Poller is where a reminder goes off. The row is marked first and the alert sent after, so a person offline still finds it in Mine.
type Poller struct {
	opts    PollerOptions
	running sync.WaitGroup
}

func NewPoller(opts PollerOptions) *Poller {
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Poller{opts: opts}
}

func (p *Poller) Start(ctx context.Context) {
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		ticker := time.NewTicker(p.opts.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.tick(ctx)
			}
		}
	}()
}

// Wait blocks until the loop returned or ctx expired, the same ordered shutdown as the turn's poller (ADR-0029).
func (p *Poller) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Poller) tick(ctx context.Context) {
	due, err := p.opts.Store.Due(ctx, p.opts.Limit)
	if err != nil {
		p.opts.Logger.Error("the due reminders could not be marked", slog.String("cause", err.Error()))
		return
	}
	for _, d := range due {
		p.opts.Alerts.Remind(d.Recipient, present(d.Reminder))
		p.opts.Logger.Info("reminder due", slog.String("reminder_id", d.ID.String()), slog.String("recipient", d.Recipient.String()))
	}
}
