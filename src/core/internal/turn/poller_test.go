package turn

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// recordingStarter stands in for the orchestrator: what the poller decides is which turns begin, and nothing about how they end.
type recordingStarter struct {
	mu      sync.Mutex
	started []Batch
	fired   chan struct{}
	once    sync.Once
}

func (r *recordingStarter) Start(_ context.Context, batch Batch) {
	r.mu.Lock()
	r.started = append(r.started, batch)
	r.mu.Unlock()
	r.once.Do(func() { close(r.fired) })
}

func (r *recordingStarter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.started)
}

// ticking builds a poller whose interval is short enough that a test never waits on the clock.
func ticking(store *stubStore, starter Starter) *Poller {
	return NewPoller(PollerOptions{
		Store:       store,
		Interval:    time.Millisecond,
		Window:      10 * time.Second,
		MaxDuration: time.Minute,
		MaxMessages: 10,
		Limit:       10,
		Turns:       starter,
		Logger:      slog.New(slog.DiscardHandler),
	})
}

// The whole of regla 7: what the poller closes is what starts a turn, and the batch travels closed, with its count and its reason.
func TestThePollerStartsATurnForEveryBatchItCloses(t *testing.T) {
	due := Batch{
		ID:             uuid.New(),
		ConversationID: uuid.New(),
		TurnNumber:     4,
		MessagesCount:  3,
		TimerMsUsed:    10000,
		ClosedReason:   enums.BatchCloseReasonSilence,
	}
	store := &stubStore{due: []Batch{due}}
	starter := &recordingStarter{fired: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticking(store, starter).Start(ctx)

	select {
	case <-starter.fired:
	case <-time.After(2 * time.Second):
		t.Fatal("no turn started for a batch that was due: the timer of F10 never goes off")
	}

	starter.mu.Lock()
	defer starter.mu.Unlock()
	got := starter.started[0]
	if got.ID != due.ID || got.MessagesCount != 3 || got.ClosedReason != enums.BatchCloseReasonSilence {
		t.Fatalf("the turn began with %+v, want the batch as it was closed", got)
	}
}

// A tick that fails is logged and nothing else: the batches it did not close are still due, and the next tick asks the same.
func TestThePollerKeepsTickingAfterAFailedTick(t *testing.T) {
	store := &stubStore{closeErr: errors.New("db: connection reset")}
	starter := &recordingStarter{fired: make(chan struct{})}
	poller := ticking(store, starter)

	ctx, cancel := context.WithCancel(context.Background())
	poller.Start(ctx)

	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := poller.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: got %v, want the loop to be out", err)
	}
	if starter.count() != 0 {
		t.Fatal("a turn started although no batch was ever closed")
	}
}

// Regla 11: the shutdown stops the loop before it waits for the turns, and it has to be able to tell that it stopped. A poller
// still ticking would keep adding turns to what the deployment has to wait for.
func TestThePollerStopsWhenItsContextIsCancelled(t *testing.T) {
	store := &stubStore{}
	poller := ticking(store, &recordingStarter{fired: make(chan struct{})})

	ctx, cancel := context.WithCancel(context.Background())
	poller.Start(ctx)

	// Still running: Wait must not report a loop that is out while the ticker is alive.
	immediate, cancelImmediate := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelImmediate()
	if err := poller.Wait(immediate); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait: got %v while the loop was running, want it to block", err)
	}

	cancel()
	if err := poller.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: got %v after the cancellation, want nil", err)
	}
}

// The context the turn gets is not the poller's: cancelling the loop at shutdown must not kill the turn it just handed over.
func TestTheTurnOutlivesThePollerThatStartedIt(t *testing.T) {
	store := &stubStore{due: []Batch{{ID: uuid.New(), ConversationID: uuid.New()}}}
	alive := make(chan error, 1)
	starter := &livenessStarter{alive: alive, handed: make(chan struct{})}
	poller := ticking(store, starter)

	ctx, cancel := context.WithCancel(context.Background())
	poller.Start(ctx)

	select {
	case <-starter.handed:
	case <-time.After(2 * time.Second):
		t.Fatal("the poller never handed a batch over")
	}
	cancel()
	_ = poller.Wait(context.Background())

	if err := <-alive; err != nil {
		t.Fatalf("the turn's context died with the poller: %v", err)
	}
}

// livenessStarter keeps the context it was handed and reports, after the poller stopped, whether it is still usable.
type livenessStarter struct {
	handed chan struct{}
	alive  chan error
	ctx    context.Context
	once   sync.Once
}

func (l *livenessStarter) Start(ctx context.Context, _ Batch) {
	l.once.Do(func() {
		l.ctx = ctx
		close(l.handed)
		go func() {
			// Long enough that the cancellation below lands first if the context were the poller's own.
			time.Sleep(30 * time.Millisecond)
			l.alive <- l.ctx.Err()
		}()
	})
}
