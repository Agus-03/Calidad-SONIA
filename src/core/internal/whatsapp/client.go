package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
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

// FailureCode is what messages.error_code keeps of a send that did not leave: Meta's own code when Meta refused it, and a fixed word
// of ours when the failure happened before or around the call. The whole cause belongs in the log and not on the row, because the
// inbox shows the row to the operator and the body Meta answers with is neither short nor written for them.
//
// The two families cannot collide: Meta's codes are digits and ours are words.
func FailureCode(err error) string {
	var status *httpclient.StatusError
	var network net.Error
	switch {
	case errors.As(err, &status):
		if code := metaCode(status.Body); code != "" {
			return code
		}
		return "http_" + strconv.Itoa(status.Status)
	case errors.Is(err, ErrInboxNotFound):
		return "inbox_not_found"
	case errors.Is(err, ErrInboxNotConfigured):
		return "inbox_not_configured"
	case errors.Is(err, ErrEmptyMessage):
		return "empty_message"
	case errors.Is(err, httpclient.ErrMalformedResponse):
		return "malformed_response"
	// Before the network case: both context errors satisfy net.Error once the transport wraps them, and would read as unreachable.
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &network):
		if network.Timeout() {
			return "timeout"
		}
		return "unreachable"
	default:
		return "unclassified"
	}
}

// metaCode reads the one field of Meta's error envelope the row keeps: {"error": {"code": 131047, ...}}.
func metaCode(body string) string {
	var envelope struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil || envelope.Error.Code == 0 {
		return ""
	}
	return strconv.Itoa(envelope.Error.Code)
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
