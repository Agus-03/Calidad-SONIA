package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// ErrInboxNotConfigured is a state of the data and not of the deployment: a number without a token is one nobody connected yet (F29).
var ErrInboxNotConfigured = errors.New("whatsapp: the inbox has no access token")

// ErrEmptyMessage guards the channel from a turn that generated nothing: Meta rejects an empty body with a 400 anyway.
var ErrEmptyMessage = errors.New("whatsapp: refusing to send an empty message")

// Declared here because whatsapp/ is the only package that knows what sending means, and it never retries (ADR-0020).
type Sender interface {
	Send(ctx context.Context, inbox Inbox, to, text string) (string, error)
}

type ClientOptions struct {
	GraphBaseURL string
	APIVersion   string
	Timeout      time.Duration
	HTTP         httpclient.Doer
	Logger       *slog.Logger
}

type Client struct {
	http    *httpclient.Client
	version string
	log     *slog.Logger
}

func NewClient(opts ClientOptions) *Client {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{
		http: httpclient.New(httpclient.Options{
			BaseURL: opts.GraphBaseURL,
			Timeout: opts.Timeout,
			HTTP:    opts.HTTP,
		}),
		version: opts.APIVersion,
		log:     opts.Logger,
	}
}

// The shape Meta's Cloud API declares for a free form text message inside the 24 hour service window.
type sendRequest struct {
	MessagingProduct string      `json:"messaging_product"`
	RecipientType    string      `json:"recipient_type"`
	To               string      `json:"to"`
	Type             string      `json:"type"`
	Text             sendMessage `json:"text"`
}

type sendMessage struct {
	// Off because a link preview rewrites what the agent wrote, and F36 measures the answer that was generated.
	PreviewURL bool   `json:"preview_url"`
	Body       string `json:"body"`
}

type sendResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

// Send returns the wamid Meta assigns, which is what messages.wamid was left nullable to receive (07-secuencia, hallazgo 3).
func (c *Client) Send(ctx context.Context, inbox Inbox, to, text string) (string, error) {
	if inbox.AccessToken == "" {
		return "", ErrInboxNotConfigured
	}
	if text == "" {
		return "", ErrEmptyMessage
	}

	body := sendRequest{
		MessagingProduct: "whatsapp",
		RecipientType:    "individual",
		To:               to,
		Type:             "text",
		Text:             sendMessage{Body: text},
	}

	var res sendResponse
	path := "/" + c.version + "/" + inbox.PhoneNumberID + "/messages"
	// The token travels per call and not per client, because it belongs to the inbox row and one core serves several.
	err := c.http.PostJSON(ctx, path, body, &res, httpclient.Header("Authorization", "Bearer "+inbox.AccessToken))
	if err != nil {
		return "", fmt.Errorf("whatsapp: send to %s: %w", inbox.PhoneNumberID, err)
	}

	if len(res.Messages) == 0 {
		return "", fmt.Errorf("whatsapp: send to %s: %w", inbox.PhoneNumberID, httpclient.ErrMalformedResponse)
	}
	return res.Messages[0].ID, nil
}

// DoubleSender is the only way Fase 1 runs at all, and config refuses it outside development and test.
type DoubleSender struct{ log *slog.Logger }

func NewDoubleSender(log *slog.Logger) *DoubleSender {
	if log == nil {
		log = slog.Default()
	}
	return &DoubleSender{log: log}
}

func (d *DoubleSender) Send(_ context.Context, inbox Inbox, to, text string) (string, error) {
	if text == "" {
		return "", ErrEmptyMessage
	}

	// Prefixed so nobody mistakes it for Meta's, and unique so the wamid uniqueness of the schema still holds across turns.
	wamid := "wamid.double." + id.New().String()
	d.log.Warn("whatsapp send doubled",
		slog.String("inbox_id", inbox.ID.String()),
		slog.String("to", to),
		slog.String("wamid", wamid),
	)
	return wamid, nil
}
