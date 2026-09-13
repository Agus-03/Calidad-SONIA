// Package brainclient is the core's outgoing port to the brain: the only place that knows the shape of that hop.
package brainclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

// ErrTurnNotFound is the brain's 404: the four ids name no turn, which is our mistake and never gets better on a retry.
var ErrTurnNotFound = errors.New("brainclient: the brain does not know that turn")

// The whole authentication between the two deployables (ADR-0017 (k)); there is no user on this hop.
const tokenHeader = "X-Internal-Token"

type Options struct {
	BaseURL string
	Token   string
	Timeout time.Duration
	HTTP    httpclient.Doer
	Logger  *slog.Logger
}

type Client struct {
	http *httpclient.Client
	log  *slog.Logger
}

func New(opts Options) *Client {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		http: httpclient.New(httpclient.Options{
			BaseURL: opts.BaseURL,
			Timeout: opts.Timeout,
			Headers: map[string]string{tokenHeader: opts.Token},
			HTTP:    opts.HTTP,
		}),
		log: opts.Logger,
	}
}

// Turn asks the brain for the answer. It does not retry: that is the orchestrator's decision, which knows about the transaction.
func (c *Client) Turn(ctx context.Context, req TurnRequest) (TurnResponse, error) {
	var res TurnResponse

	err := c.http.PostJSON(ctx, turnPath, req, &res)
	if err != nil {
		conversation := slog.String("conversation_id", req.ConversationID.String())
		cause := slog.String("cause", err.Error())

		var status *httpclient.StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			// Wrapped and not replaced: the caller asks errors.Is what happened and Retryable what to do, and a bare sentinel loses the 404.
			c.log.Warn("brain does not know the turn", conversation, cause)
			return TurnResponse{}, fmt.Errorf("%w: %w", ErrTurnNotFound, err)
		}

		c.log.Error("brain call failed", conversation, cause)
		return TurnResponse{}, err
	}

	c.log.Info("brain answered",
		slog.String("conversation_id", req.ConversationID.String()),
		slog.String("outcome", res.Outcome),
	)
	return res, nil
}

// Models reads the priced models. Asked on every save and never cached: pricing.yaml changes with a deploy of the brain, not of the core.
func (c *Client) Models(ctx context.Context) (ModelsResponse, error) {
	var res ModelsResponse
	if err := c.http.GetJSON(ctx, modelsPath, &res); err != nil {
		c.log.Error("brain models call failed", slog.String("cause", err.Error()))
		return ModelsResponse{}, err
	}
	return res, nil
}
