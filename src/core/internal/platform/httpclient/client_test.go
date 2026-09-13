package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type payload struct {
	Name string `json:"name"`
}

func TestPostJSONSendsTheHeadersAndTheBody(t *testing.T) {
	var gotPath, gotToken, gotType, gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken, gotType = r.URL.Path, r.Header.Get("X-Internal-Token"), r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_, _ = w.Write([]byte(`{"name":"sonia"}`))
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL + "/", Headers: map[string]string{"X-Internal-Token": "secreto"}})

	var out payload
	if err := client.PostJSON(context.Background(), "/turns", payload{Name: "hola"}, &out); err != nil {
		t.Fatalf("PostJSON() error = %v, want nil", err)
	}

	// The trailing slash of BaseURL is trimmed, or every call would hit a doubled slash the router does not declare.
	if gotPath != "/turns" {
		t.Fatalf("path = %q, want %q", gotPath, "/turns")
	}
	if gotToken != "secreto" {
		t.Fatalf("token header = %q, want %q", gotToken, "secreto")
	}
	if gotType != "application/json" {
		t.Fatalf("content-type = %q, want application/json", gotType)
	}
	if gotBody != `{"name":"hola"}` {
		t.Fatalf("body = %q, want %q", gotBody, `{"name":"hola"}`)
	}
	if out.Name != "sonia" {
		t.Fatalf("out.Name = %q, want %q", out.Name, "sonia")
	}
}

// Meta's token belongs to the inbox row and one core serves several, so it cannot be fixed when the client is built.
func TestPostJSONLetsOneCallSetItsOwnHeader(t *testing.T) {
	var gotAuth, gotFixed string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotFixed = r.Header.Get("Authorization"), r.Header.Get("X-Fixed")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, Headers: map[string]string{"X-Fixed": "siempre", "Authorization": "Bearer viejo"}})

	err := client.PostJSON(context.Background(), "/messages", payload{}, nil, Header("Authorization", "Bearer nuevo"))
	if err != nil {
		t.Fatalf("PostJSON() error = %v, want nil", err)
	}

	// The per-call value wins, and what was fixed at construction survives untouched.
	if gotAuth != "Bearer nuevo" {
		t.Fatalf("Authorization = %q, want the per-call value", gotAuth)
	}
	if gotFixed != "siempre" {
		t.Fatalf("X-Fixed = %q, want the value fixed at construction", gotFixed)
	}
}

func TestPostJSONReturnsTheStatusAndTheBodyItGotBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":"unavailable"}`))
	}))
	defer server.Close()

	err := New(Options{BaseURL: server.URL}).PostJSON(context.Background(), "/turns", payload{}, &payload{})

	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("error = %v, want a *StatusError", err)
	}
	if status.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", status.Status, http.StatusServiceUnavailable)
	}
	// The body travels because it is what the log needs to say why the other side refused.
	if status.Body != `{"code":"unavailable"}` {
		t.Fatalf("body = %q, want the payload the service sent", status.Body)
	}
}

// A service that answers badly must not fill the core's memory, and the cap has to hold on the error path too.
func TestPostJSONCapsWhatItReadsBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", maxBodyBytes+4096)))
	}))
	defer server.Close()

	err := New(Options{BaseURL: server.URL}).PostJSON(context.Background(), "/turns", payload{}, &payload{})

	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("error = %v, want a *StatusError", err)
	}
	if len(status.Body) != maxBodyBytes {
		t.Fatalf("body length = %d, want %d", len(status.Body), maxBodyBytes)
	}
}

func TestPostJSONNamesABodyThatIsNotTheContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>oops</html>"))
	}))
	defer server.Close()

	err := New(Options{BaseURL: server.URL}).PostJSON(context.Background(), "/turns", payload{}, &payload{})

	if !errors.Is(err, ErrMalformedResponse) {
		t.Fatalf("error = %v, want ErrMalformedResponse", err)
	}
}

// A caller that wants no body must not be forced to invent a struct to throw away.
func TestPostJSONSkipsDecodingWhenNothingIsWanted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("no soy json"))
	}))
	defer server.Close()

	if err := New(Options{BaseURL: server.URL}).PostJSON(context.Background(), "/turns", payload{}, nil); err != nil {
		t.Fatalf("PostJSON() error = %v, want nil", err)
	}
}

// The deadline is the caller's, not the transport's: whoever waits decides how long it is worth waiting.
func TestPostJSONStopsAtItsOwnTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := New(Options{BaseURL: server.URL, Timeout: 30 * time.Millisecond})
	err := client.PostJSON(context.Background(), "/turns", payload{}, &payload{})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if !Retryable(err) {
		t.Fatal("a timeout never reached an answer, so it has to stay retryable")
	}
}

// The message is what ends up in the log and in the operator's view, so it has to carry both halves of what happened.
func TestStatusErrorNamesTheStatusAndWhatCameBack(t *testing.T) {
	err := &StatusError{Status: 400, Body: `{"error":{"code":131047}}`}

	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "131047") {
		t.Fatalf("Error() = %q, want the status and the body in it", err.Error())
	}
}

func TestRetryableSeparatesARejectionFromAFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"a 400 will not get better by asking again: the request itself is what was refused", &StatusError{Status: 400}, false},
		{"a 404 is the same: four ids that name nothing keep naming nothing", &StatusError{Status: 404}, false},
		{"a 422 is the contract being violated, and the contract does not change between two calls", &StatusError{Status: 422}, false},
		{"a 500 is the other side falling over, which is exactly what a retry is for", &StatusError{Status: 500}, true},
		{"a 503 is the other side saying not now", &StatusError{Status: 503}, true},
		{"a transport failure never reached the service, so it is the same kind of failure as a 5xx", errors.New("connection refused"), true},
		{"a body that is not the contract will not become the contract on a second try", ErrMalformedResponse, false},
		{"a cancelled context is the caller giving up, and a retry would ignore that", context.Canceled, false},
		{"nothing failed", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Retryable(tc.err); got != tc.want {
				t.Fatalf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// Nothing in this package loops: retrying is the turn's decision (ADR-0020), which is the only one that knows the transaction.
func TestPostJSONCallsOnceAndOnlyOnce(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_ = New(Options{BaseURL: server.URL}).PostJSON(context.Background(), "/turns", payload{}, &payload{})

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// A read carries the shared secret like a write, and sends no Content-Type because it sends no body.
func TestGetJSONSendsTheHeadersAndDecodesTheAnswer(t *testing.T) {
	var gotMethod, gotPath, gotToken, gotType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotToken, gotType = r.Header.Get("X-Internal-Token"), r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"name":"sonia"}`))
	}))
	defer server.Close()

	client := New(Options{BaseURL: server.URL, Headers: map[string]string{"X-Internal-Token": "secreto"}})

	var out payload
	if err := client.GetJSON(context.Background(), "/models", &out); err != nil {
		t.Fatalf("GetJSON() error = %v, want nil", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/models" {
		t.Fatalf("request = %s %s, want GET /models", gotMethod, gotPath)
	}
	if gotToken != "secreto" {
		t.Fatalf("token header = %q, want %q", gotToken, "secreto")
	}
	if gotType != "" {
		t.Fatalf("content-type = %q, want none on a request with no body", gotType)
	}
	if out.Name != "sonia" {
		t.Fatalf("out.Name = %q, want %q", out.Name, "sonia")
	}
}

// The status travels as a type on a read too, because the caller decides between a refusal and an outage the same way.
func TestGetJSONReturnsTheStatusItGotBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := New(Options{BaseURL: server.URL}).GetJSON(context.Background(), "/models", &payload{})

	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusServiceUnavailable {
		t.Fatalf("GetJSON() error = %v, want a StatusError with 503", err)
	}
}
