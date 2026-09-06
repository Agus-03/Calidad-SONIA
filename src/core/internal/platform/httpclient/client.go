// Package httpclient is the outgoing side of the boundaries the core calls: it classifies a failure, it never decides on it.
// Retrying is the turn's call (ADR-0020), so nothing here loops: a retry hidden in the transport cannot know about the transaction.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrMalformedResponse marks a 2xx whose body is not the contract; it is not retryable, because it will not become the contract.
var ErrMalformedResponse = errors.New("httpclient: response body is not the declared contract")

// maxBodyBytes caps what a misbehaving service can push into the core's memory.
const maxBodyBytes = 1 << 20

// StatusError is the answer that was not a 2xx. It is a type and not a sentinel because the caller has to decide on the status.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("httpclient: status %d: %s", e.Status, e.Body)
}

// Retryable is the whole reason this package exists: a 4xx will not get better by asking again, a 5xx might.
func (e *StatusError) Retryable() bool { return e.Status >= http.StatusInternalServerError }

// Doer is the consumer-side interface: a test needs no listening socket, and http.Client already satisfies it.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

type Options struct {
	BaseURL string
	Timeout time.Duration
	// Sent on every call: the shared secret of the brain, the bearer token of Meta.
	Headers map[string]string
	HTTP    Doer
}

type Client struct {
	base    string
	timeout time.Duration
	headers map[string]string
	http    Doer
}

func New(opts Options) *Client {
	if opts.HTTP == nil {
		opts.HTTP = &http.Client{}
	}
	return &Client{
		base:    strings.TrimSuffix(opts.BaseURL, "/"),
		timeout: opts.Timeout,
		headers: opts.Headers,
		http:    opts.HTTP,
	}
}

// Option customises one call. The brain's shared secret is the same on every request, but Meta's token belongs to the inbox.
type Option func(req *http.Request)

func Header(name, value string) Option {
	return func(req *http.Request) { req.Header.Set(name, value) }
}

// PostJSON sends in and decodes out, with its own deadline: whoever waits decides how long, not the transport.
func (c *Client) PostJSON(ctx context.Context, path string, in, out any, opts ...Option) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("httpclient: encode %s: %w", path, err)
	}

	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("httpclient: build %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	// Applied last so a per-call value wins over the one fixed at construction.
	for _, opt := range opts {
		opt(req)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("httpclient: call %s: %w", path, err)
	}
	defer func() { _ = res.Body.Close() }()

	// Bounded before anything reads it: a service that answers badly must not fill the core's memory.
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("httpclient: read %s: %w", path, err)
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &StatusError{Status: res.StatusCode, Body: string(payload)}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("httpclient: decode %s: %w: %w", path, ErrMalformedResponse, err)
	}
	return nil
}

// Retryable classifies; it does not retry. A call that never reached the service is the same kind of failure as a 5xx.
func Retryable(err error) bool {
	if err == nil {
		return false
	}

	var status *StatusError
	if errors.As(err, &status) {
		return status.Retryable()
	}
	if errors.Is(err, ErrMalformedResponse) {
		return false
	}
	// A cancelled context is the caller giving up, not the service failing: asking again would ignore that.
	return !errors.Is(err, context.Canceled)
}
