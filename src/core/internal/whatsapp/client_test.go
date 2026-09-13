package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

const sendAccepted = `{"messaging_product":"whatsapp","contacts":[{"input":"5493510000000","wa_id":"5493510000000"}],` +
	`"messages":[{"id":"wamid.HBgNNTQ5MzUxMDAwMDAwMBUCABEYEjlDN0Y="}]}`

func connected() Inbox {
	return Inbox{ID: uuid.New(), AccountID: uuid.New(), PhoneNumberID: "000000000000000", AccessToken: "EAAG-token"}
}

func TestSendSpeaksTheShapeMetaDeclares(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(sendAccepted))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"})

	wamid, err := client.Send(context.Background(), connected(), "5493510000000", "hola")
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}

	// The version is pinned in the path: Meta versions its API there, and an unpinned one changes the contract without a deploy.
	if gotPath != "/v21.0/000000000000000/messages" {
		t.Fatalf("path = %q, want /v21.0/000000000000000/messages", gotPath)
	}
	// The token is the inbox's and not the deployment's, so it travels on the call and not on the client (F29).
	if gotAuth != "Bearer EAAG-token" {
		t.Fatalf("Authorization = %q, want the inbox token", gotAuth)
	}
	for field, want := range map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                "5493510000000",
		"type":              "text",
	} {
		if gotBody[field] != want {
			t.Fatalf("%s = %v, want %v", field, gotBody[field], want)
		}
	}
	text, _ := gotBody["text"].(map[string]any)
	if text["body"] != "hola" {
		t.Fatalf("text.body = %v, want hola", text["body"])
	}
	// Off because a link preview rewrites what the agent wrote, and F36 measures the answer that was generated.
	if text["preview_url"] != false {
		t.Fatalf("text.preview_url = %v, want false", text["preview_url"])
	}
	if wamid != "wamid.HBgNNTQ5MzUxMDAwMDAwMBUCABEYEjlDN0Y=" {
		t.Fatalf("wamid = %q, want the id Meta assigned", wamid)
	}
}

// A number nobody connected yet is a state of the data: the process serves the rest of the inboxes and this one fails visibly.
func TestSendRefusesAnInboxWithNoToken(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	inbox := connected()
	inbox.AccessToken = ""

	_, err := NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"}).
		Send(context.Background(), inbox, "5493510000000", "hola")

	if !errors.Is(err, ErrInboxNotConfigured) {
		t.Fatalf("error = %v, want ErrInboxNotConfigured", err)
	}
	if calls != 0 {
		t.Fatal("an inbox with no token must not reach Meta at all")
	}
}

// Ready is the same refusal asked before anything is written, and the double does not share it: that difference is what lets the
// smoke send from the demo number, which has no token, while production refuses it up front (ADR-0035, regla 9).
func TestReadyIsTheSendersToSay(t *testing.T) {
	unconnected := connected()
	unconnected.AccessToken = ""
	client := NewClient(ClientOptions{})

	if err := client.Ready(unconnected); !errors.Is(err, ErrInboxNotConfigured) {
		t.Fatalf("client.Ready(no token) = %v, want ErrInboxNotConfigured", err)
	}
	if err := client.Ready(connected()); err != nil {
		t.Fatalf("client.Ready(token) = %v, want nil", err)
	}
	if err := NewDoubleSender(nil).Ready(unconnected); err != nil {
		t.Fatalf("double.Ready(no token) = %v, want nil", err)
	}
}

func TestSendRefusesAnEmptyMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("an empty message must not reach Meta")
	}))
	defer server.Close()

	_, err := NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"}).
		Send(context.Background(), connected(), "5493510000000", "")

	if !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("error = %v, want ErrEmptyMessage", err)
	}
}

// The status is what says whether the failure can be acted on; the turn is the one that decides, and it needs the difference.
func TestSendPassesMetasStatusUp(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantRetryable bool
	}{
		{"a 400 is Meta refusing the request itself, and it refuses it again on the second call", http.StatusBadRequest,
			`{"error":{"message":"(#131047) Message failed to send","code":131047}}`, false},
		{"a 401 is a token that expired, which a retry does not renew", http.StatusUnauthorized, `{"error":{}}`, false},
		{"a 429 is the rate limit, which is Meta asking to wait and not refusing", http.StatusTooManyRequests, `{"error":{}}`, false},
		{"a 500 is Meta falling over, which is the one case where asking again can work", http.StatusInternalServerError, `{}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"}).
				Send(context.Background(), connected(), "5493510000000", "hola")

			var status *httpclient.StatusError
			if !errors.As(err, &status) {
				t.Fatalf("error = %v, want a *httpclient.StatusError", err)
			}
			if status.Status != tc.status {
				t.Fatalf("status = %d, want %d", status.Status, tc.status)
			}
			if got := httpclient.Retryable(err); got != tc.wantRetryable {
				t.Fatalf("Retryable() = %v, want %v", got, tc.wantRetryable)
			}
			// The reason Meta gave travels in the error, so the log keeps what the code on the row cannot say (ADR-0020).
			if tc.status == http.StatusBadRequest && !strings.Contains(err.Error(), "131047") {
				t.Fatalf("error = %v, want Meta's reason in it", err)
			}
		})
	}
}

// A 200 with no message id is not the contract, and inventing an empty wamid would leave a row nobody can trace back.
func TestSendRefusesAnAcceptanceWithNoWamid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"messaging_product":"whatsapp","messages":[]}`))
	}))
	defer server.Close()

	_, err := NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"}).
		Send(context.Background(), connected(), "5493510000000", "hola")

	if !errors.Is(err, httpclient.ErrMalformedResponse) {
		t.Fatalf("error = %v, want ErrMalformedResponse", err)
	}
}

// ADR-0020 sends at most once: a duplicate answer to a customer is worse than one that never arrived.
func TestSendNeverAsksTwice(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _ = NewClient(ClientOptions{GraphBaseURL: server.URL, APIVersion: "v21.0"}).
		Send(context.Background(), connected(), "5493510000000", "hola")

	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDoubleSenderAnswersWithoutReachingMeta(t *testing.T) {
	double := NewDoubleSender(nil)

	first, err := double.Send(context.Background(), connected(), "5493510000000", "hola")
	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	second, _ := double.Send(context.Background(), connected(), "5493510000000", "hola de nuevo")

	// Prefixed so nobody reads it as Meta's, and different every time so the wamid uniqueness of the schema still holds.
	if !strings.HasPrefix(first, "wamid.double.") {
		t.Fatalf("wamid = %q, want it to announce itself as doubled", first)
	}
	if first == second {
		t.Fatal("two doubled sends produced the same wamid, which the unique index would reject")
	}
}

// The same refusal as the real client: a double that accepts what Meta rejects stops being a stand in for it.
func TestDoubleSenderRefusesAnEmptyMessageToo(t *testing.T) {
	if _, err := NewDoubleSender(nil).Send(context.Background(), connected(), "5493510000000", ""); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("error = %v, want ErrEmptyMessage", err)
	}
}

// The double stands in for the client, so the turn cannot tell them apart: that is the whole point of the seam.
func TestBothSendersSatisfyTheSamePort(t *testing.T) {
	var _ Sender = NewClient(ClientOptions{})
	var _ Sender = NewDoubleSender(nil)
}

// sendThrough returns the error the real client builds, so the code is read from what Send hands the turn and the inbox, not from
// an error written to look like it.
func sendThrough(t *testing.T, baseURL string) error {
	t.Helper()
	_, err := NewClient(ClientOptions{GraphBaseURL: baseURL, APIVersion: "v21.0"}).
		Send(context.Background(), connected(), "5493510000000", "hola")
	if err == nil {
		t.Fatal("Send() error = nil, want the failure to classify")
	}
	return err
}

// TestFailureCodeKeepsACodeAndNotTheCause is what messages.error_code holds: the operator reads it in the inbox, so it is Meta's code
// or one fixed word, never a sentence with a response body inside (ADR-0020).
func TestFailureCodeKeepsACodeAndNotTheCause(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"(#131047) Re-engagement message","type":"OAuthException","code":131047}}`))
	}))
	defer refusing.Close()

	gone := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gone.Close()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"Meta refusing keeps Meta's own code, the one its documentation explains", sendThrough(t, refusing.URL), "131047"},
		{"a status with no envelope keeps the status, because there is no code of Meta's to keep",
			&httpclient.StatusError{Status: http.StatusBadGateway, Body: "<html>bad gateway</html>"}, "http_502"},
		{"an envelope with no code is the same as no envelope",
			&httpclient.StatusError{Status: http.StatusUnauthorized, Body: `{"error":{"message":"expired"}}`}, "http_401"},
		{"a number that does not exist never reached Meta", fmt.Errorf("turn: %w", ErrInboxNotFound), "inbox_not_found"},
		{"a number nobody connected never reached Meta either", fmt.Errorf("turn: %w", ErrInboxNotConfigured), "inbox_not_configured"},
		{"an empty message is refused before the call", ErrEmptyMessage, "empty_message"},
		{"a 200 that is not the contract", fmt.Errorf("whatsapp: send: %w", httpclient.ErrMalformedResponse), "malformed_response"},
		{"a deadline the transport wraps is a timeout and not a lost network",
			&url.Error{Op: "Post", URL: "https://graph", Err: context.DeadlineExceeded}, "timeout"},
		{"a cancellation the transport wraps is somebody giving up, not the network",
			&url.Error{Op: "Post", URL: "https://graph", Err: context.Canceled}, "canceled"},
		{"a network timeout without a context is still a timeout", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, "timeout"},
		{"nobody listening is a network that could not be reached", sendThrough(t, gone.URL), "unreachable"},
		{"anything else says so, and the cause is in the log",
			errors.New("whatsapp: decrypt the access token: cipher: message authentication failed"), "unclassified"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FailureCode(tc.err); got != tc.want {
				t.Fatalf("FailureCode(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
