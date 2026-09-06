package turn

import (
	"context"
	"errors"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/brainclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

// Do is the whole retry policy of the core, here and not in the transport: httpclient classifies, this decides (ADR-0020).
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

// The turn's own reading on top of the transport's: no number of calls will make those four ids name a turn.
func worthAskingAgain(err error) bool {
	if errors.Is(err, brainclient.ErrTurnNotFound) {
		return false
	}
	return httpclient.Retryable(err)
}
