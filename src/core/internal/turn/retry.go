package turn

import (
	"context"
	"errors"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

// Do is the whole retry policy of the core, and it lives here and not in the transport: httpclient classifies, this decides.
// Only what httpclient calls retryable is asked again: a 4xx will not get better, and asking spends the turn's budget.
func Do(ctx context.Context, attempts int, backoff time.Duration, call func(context.Context) error) error {
	var err error

	for attempt := 0; attempt <= attempts; attempt++ {
		if attempt > 0 {
			// Linear and waiting on ctx too, so a cancelled turn stops sleeping instead of finishing its schedule.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * backoff):
			}
		}

		if err = call(ctx); err == nil {
			return nil
		}
		if !worthAskingAgain(err) {
			return err
		}
	}
	return err
}

// The turn's own reading on top of the transport's classification, because deciding is this package's job and not httpclient's.
// httpclient calls an error it does not recognise retryable, which is right for a transport failure and wrong for a domain
// error that happens to travel through it: no number of calls will make those four ids name a turn.
func worthAskingAgain(err error) bool {
	if errors.Is(err, brainclient.ErrTurnNotFound) {
		return false
	}
	return httpclient.Retryable(err)
}
