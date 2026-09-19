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

// ErrAIAgentNotFound is the preview's 404: the agent was removed between the core's check and the brain's read.
var ErrAIAgentNotFound = errors.New("brainclient: the brain does not know that ai agent")

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

// Preview asks the brain for the system the model would read with these blocks. No retry: an administrator is typing, and the next
// keystroke asks again.
func (c *Client) Preview(ctx context.Context, req PreviewRequest) (PreviewResponse, error) {
	var res PreviewResponse
	err := c.http.PostJSON(ctx, previewPath, req, &res)
	if err == nil {
		return res, nil
	}
	agent := slog.String("ai_agent_id", req.AIAgentID.String())

	var status *httpclient.StatusError
	if !errors.As(err, &status) {
		c.log.Error("brain preview call failed", agent, slog.String("cause", err.Error()))
		return PreviewResponse{}, err
	}
	// The status alone and never the body, here and in the error that travels up to the request log: a 422 of FastAPI echoes its
	// input, and the input of a preview is the text of the drafts.
	code := slog.Int("status", status.Status)
	if status.Status == http.StatusNotFound {
		c.log.Warn("brain does not know the ai agent", agent, code)
		return PreviewResponse{}, fmt.Errorf("%w: status %d", ErrAIAgentNotFound, status.Status)
	}
	if status.Retryable() {
		c.log.Error("brain preview call failed", agent, code)
	} else {
		c.log.Warn("brain refused the preview", agent, code)
	}
	return PreviewResponse{}, fmt.Errorf("brainclient: the brain answered the preview with status %d", status.Status)
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
