package turn

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Starter is the orchestrator seen from this side: the poller decides which turns begin, and nothing about how they end.
type Starter interface {
	Start(ctx context.Context, batch Batch)
}

// Poller is where the timer of F10 actually goes off. It holds no state of its own: what is due it asks PostgreSQL every tick,
// so a restart loses nothing and the batches that expired while the core was down are closed like any other (ADR-0041, regla 9).
type Poller struct {
	opts PollerOptions
	// Counts the loop itself, so the shutdown can tell it stopped before it waits for the turns in flight (regla 11).
	running sync.WaitGroup
}

type PollerOptions struct {
	Store Store

	// How often the question is asked, and the three numbers the close is decided with (ADR-0041, reglas 4, 5 y 10).
	Interval    time.Duration
	Window      time.Duration
	MaxDuration time.Duration
	MaxMessages int
	// At most this many batches per tick; see Repository.Close for why it is not a queue.
	Limit int

	Turns  Starter
	Logger *slog.Logger
}

func NewPoller(opts PollerOptions) *Poller {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Poller{opts: opts}
}

// Start registers the loop before spawning it, for the same reason the orchestrator does: a shutdown that begins the instant
// this returns has to find the poller running, or it would wait for the turns without having stopped the thing that opens them.
func (p *Poller) Start(ctx context.Context) {
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		p.loop(ctx)
	}()
}

func (p *Poller) loop(ctx context.Context) {
	ticker := time.NewTicker(p.opts.Interval)
	defer ticker.Stop()

	p.opts.Logger.Info("turn poller started", slog.Int64("interval_ms", p.opts.Interval.Milliseconds()))

	for {
		select {
		case <-ctx.Done():
			p.opts.Logger.Info("turn poller stopped")
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

// Wait blocks until the loop is out, which is what lets the shutdown stop opening turns before it waits for the ones open.
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

// A failed tick is logged and nothing else: the batches it did not close are still due, and the next tick asks the same question.
func (p *Poller) tick(ctx context.Context) {
	closed, err := p.opts.Store.Close(ctx, p.opts.Window, p.opts.MaxDuration, p.opts.MaxMessages, p.opts.Limit)
	if err != nil {
		p.opts.Logger.Error("the due batches could not be closed", slog.String("cause", err.Error()))
		return
	}

	for _, batch := range closed {
		p.opts.Logger.Info("turn batch closed",
			slog.String("batch_id", batch.ID.String()),
			slog.String("closed_reason", string(batch.ClosedReason)),
			slog.Int("messages_count", batch.MessagesCount),
		)
		// Without the cancellation: this context dies when the shutdown stops the loop, and the turn it just started must live
		// long enough for Orchestrator.Wait to find it (ADR-0029).
		p.opts.Turns.Start(context.WithoutCancel(ctx), batch)
	}
}
