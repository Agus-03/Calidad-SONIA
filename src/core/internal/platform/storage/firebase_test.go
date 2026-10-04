package storage

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

// fakeStore records what reached it and answers with the status the case chose, like a fake-gcs-server that can fail.
type fakeStore struct {
	mu       sync.Mutex
	requests []recorded
	status   int
}

type recorded struct {
	method, path, rawPath, query, contentType, authorization string
	body                                                     []byte
}

func (f *fakeStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{
		method: r.Method, path: r.URL.Path, rawPath: r.URL.EscapedPath(), query: r.URL.RawQuery,
		contentType: r.Header.Get("Content-Type"), authorization: r.Header.Get("Authorization"), body: body,
	})
	status := f.status
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{}`))
}

func emulated(t *testing.T, fake *fakeStore) *Firebase {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	f, err := NewFirebase(FirebaseOptions{Bucket: "sonia-test", EmulatorHost: server.URL, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewFirebase: %v", err)
	}
	return f
}

// TestAnObjectGoesUpWholeUnderItsKeyAsCiphertext is the upload of ADR-0064, regla 6 (b), as the JSON API of Cloud Storage takes it.
func TestAnObjectGoesUpWholeUnderItsKeyAsCiphertext(t *testing.T) {
	fake := &fakeStore{}
	f := emulated(t, fake)
	key := "knowledge-bases/0191a2b3-0000-7000-8000-000000000001/0191a2b3-0000-7000-8000-000000000002"

	if err := f.Put(t.Context(), key, []byte("sealed bytes")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got := fake.requests[0]
	if got.method != http.MethodPost || got.path != "/upload/storage/v1/b/sonia-test/o" {
		t.Fatalf("Put reached %s %s, want POST to the bucket's upload endpoint", got.method, got.path)
	}
	if !strings.Contains(got.query, "uploadType=media") || !strings.Contains(got.query, "name=knowledge-bases%2F") {
		t.Fatalf("Put query %q, want a media upload named by the escaped key", got.query)
	}
	if got.contentType != "application/octet-stream" || string(got.body) != "sealed bytes" {
		t.Fatalf("Put sent %q as %q, want the bytes as they were, as an opaque stream", got.body, got.contentType)
	}
	// The emulator takes no credential, so nothing that looks like one may leave for it.
	if got.authorization != "" {
		t.Fatalf("an emulated upload carried %q, want no credential", got.authorization)
	}
}

// TestDeletingTheOrphanEscapesTheKeyAndTakesAMissingOneAsClean is ADR-0064, regla 6 (d): the cleanup of an upload whose row failed.
func TestDeletingTheOrphanEscapesTheKeyAndTakesAMissingOneAsClean(t *testing.T) {
	fake := &fakeStore{status: http.StatusNoContent}
	f := emulated(t, fake)

	if err := f.Delete(t.Context(), "knowledge-bases/a/b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := fake.requests[0]
	// The slashes of the key travel escaped: unescaped, the key would read as more segments of the path.
	if got.method != http.MethodDelete || got.rawPath != "/storage/v1/b/sonia-test/o/knowledge-bases%2Fa%2Fb" {
		t.Fatalf("Delete reached %s %s, want DELETE of the escaped key", got.method, got.rawPath)
	}

	fake.status = http.StatusNotFound
	if err := f.Delete(t.Context(), "knowledge-bases/a/b"); err != nil {
		t.Fatalf("deleting a missing object: got %v, want nil, because it is already gone", err)
	}
}

// TestAStoreThatFailsAnswersTheStatusTheCallerCanClassify: the same type every client of the core returns (httpclient).
func TestAStoreThatFailsAnswersTheStatusTheCallerCanClassify(t *testing.T) {
	fake := &fakeStore{status: http.StatusServiceUnavailable}
	f := emulated(t, fake)

	for _, err := range []error{f.Put(t.Context(), "k", []byte("x")), f.Delete(t.Context(), "k")} {
		var status *httpclient.StatusError
		if !errors.As(err, &status) || status.Status != http.StatusServiceUnavailable || !status.Retryable() {
			t.Fatalf("got %v, want a retryable 503", err)
		}
	}

	unreachable, err := NewFirebase(FirebaseOptions{Bucket: "b", EmulatorHost: "127.0.0.1:1", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewFirebase: %v", err)
	}
	if err := unreachable.Put(t.Context(), "k", []byte("x")); err == nil {
		t.Fatalf("a store nobody listens on accepted the upload")
	}
}

// TestTheEmulatorHostIsReadAsGooglesClientsReadIt: with or without a scheme, which is what lets one variable serve both services.
func TestTheEmulatorHostIsReadAsGooglesClientsReadIt(t *testing.T) {
	for in, want := range map[string]string{
		"storage:4443":         "http://storage:4443",
		"http://storage:4443/": "http://storage:4443",
		"https://gcs.test":     "https://gcs.test",
	} {
		if got := emulatorURL(in); got != want {
			t.Fatalf("emulatorURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTheBootRefusesAStoreItCannotWriteTo: a bucket, a readable file and a credential Google accepts, or the process does not
// start (ADR-0069, and regla 10 of 08-structure-rules).
func TestTheBootRefusesAStoreItCannotWriteTo(t *testing.T) {
	if _, err := NewFirebase(FirebaseOptions{EmulatorHost: "storage:4443"}); err == nil {
		t.Fatalf("NewFirebase without a bucket was accepted")
	}
	if _, err := NewFirebase(FirebaseOptions{Bucket: "b", CredentialsFile: filepath.Join(t.TempDir(), "missing.json")}); err == nil {
		t.Fatalf("NewFirebase with a missing credential file was accepted")
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte(`{"type":"nothing"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewFirebase(FirebaseOptions{Bucket: "b", CredentialsFile: broken}); err == nil {
		t.Fatalf("NewFirebase with a file that is not a service account was accepted")
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	t.Cleanup(refusing.Close)
	if _, err := NewFirebase(FirebaseOptions{Bucket: "b", CredentialsFile: serviceAccount(t, refusing.URL)}); err == nil {
		t.Fatalf("NewFirebase with a credential Google refuses was accepted")
	}
}

// TestATokenEndpointThatNeverAnswersDoesNotHangTheBoot: the exchange has a client of its own with a timeout, so an endpoint that
// accepts and never answers fails the boot instead of holding it, and later every upload behind the token source's lock.
func TestATokenEndpointThatNeverAnswersDoesNotHangTheBoot(t *testing.T) {
	release := make(chan struct{})
	hanging := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); hanging.Close() })
	saved := tokenTimeout
	tokenTimeout = 200 * time.Millisecond
	t.Cleanup(func() { tokenTimeout = saved })

	started := time.Now()
	if _, err := NewFirebase(FirebaseOptions{Bucket: "b", CredentialsFile: serviceAccount(t, hanging.URL)}); err == nil {
		t.Fatalf("NewFirebase with a token endpoint that never answers was accepted")
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("the boot waited %v for the token, want it bounded by the exchange's own timeout", waited)
	}
}

// TestARealCredentialSignsEveryCall walks the path production takes: the service account is exchanged for a token, and the token
// rides on the upload. Google is two local servers here.
func TestARealCredentialSignsEveryCall(t *testing.T) {
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.Form.Get("assertion") == "" {
			http.Error(w, "not a service account exchange", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-de-prueba", "token_type": "Bearer", "expires_in": 3600})
	}))
	t.Cleanup(tokens.Close)

	f, err := NewFirebase(FirebaseOptions{Bucket: "sonia-prod", CredentialsFile: serviceAccount(t, tokens.URL), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewFirebase with a credential the token server accepts: %v", err)
	}
	if f.base != defaultBaseURL {
		t.Fatalf("base = %q, want Google's own host when no emulator is set", f.base)
	}

	fake := &fakeStore{}
	store := httptest.NewServer(fake)
	t.Cleanup(store.Close)
	f.base = store.URL
	if err := f.Put(t.Context(), "k", []byte("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := fake.requests[0].authorization; got != "Bearer token-de-prueba" {
		t.Fatalf("the upload carried %q, want the token the service account was exchanged for", got)
	}
}

// serviceAccount writes the JSON a service account key download looks like, with its token endpoint pointed at tokenURL.
func serviceAccount(t *testing.T, tokenURL string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	raw, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "sonia-test",
		"private_key_id": "key-1",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   "sonia-core@sonia-test.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      tokenURL,
	})
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	path := filepath.Join(t.TempDir(), "service-account.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}
