package turn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

func TestDoStopsAtTheFirstSuccess(t *testing.T) {
	calls := 0

	err := Do(context.Background(), 3, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 2 {
			return &httpclient.StatusError{Status: 503}
		}
		return nil
	})

	if err != nil || calls != 2 {
		t.Fatalf("err = %v after %d calls, want nil after 2", err, calls)
	}
}

// httpclient classifies and this decides, so the two readings have to be tested separately (ADR of the split, struct.md:155).
func TestWorthAskingAgainSeparatesWhatARetryCanFix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a 500 is the brain falling over, which is what a retry is for", &httpclient.StatusError{Status: 500}, true},
		{"a transport failure never reached the brain", errors.New("connection refused"), true},
		{"a 422 is the contract being violated, which two calls do not fix", &httpclient.StatusError{Status: 422}, false},
		{"no number of calls will make four ids name a turn that does not exist", brainclient.ErrTurnNotFound, false},
		{"a cancelled turn is the caller giving up", context.Canceled, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := worthAskingAgain(tc.err); got != tc.want {
				t.Fatalf("worthAskingAgain(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The backoff waits on the context too, so a turn that ran out of budget stops instead of finishing its schedule.
func TestDoStopsSleepingWhenTheTurnIsOver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	start := time.Now()
	err := Do(ctx, 3, time.Hour, func(context.Context) error {
		calls++
		cancel()
		return &httpclient.StatusError{Status: 503}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 || time.Since(start) > time.Second {
		t.Fatalf("%d calls in %v: the backoff was slept through instead of being cut", calls, time.Since(start))
	}
}

// Zero retries has to mean one attempt and not zero, or disabling retries would disable the turn.
func TestDoStillCallsOnceWithRetriesDisabled(t *testing.T) {
	calls := 0

	_ = Do(context.Background(), 0, time.Millisecond, func(context.Context) error {
		calls++
		return &httpclient.StatusError{Status: 503}
	})

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
