// Package storage is the adapter of the fourth frontier with a port: where the bytes of a document live (ADR-0017, ADR-0019).
// The port itself is declared by its caller, documents/, as every interface of the core is.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/httpclient"
)

// The JSON API of Cloud Storage, which is what Cloud Storage for Firebase is underneath (ADR-0069).
const defaultBaseURL = "https://storage.googleapis.com"

// Read and write and nothing more: the core uploads and deletes objects, and never manages buckets.
const readWriteScope = "https://www.googleapis.com/auth/devstorage.read_write"

// tokenTimeout bounds one exchange of the service account for a token: Google answers it in well under a second. A variable
// only so a test can wait less than this for an endpoint that never answers.
var tokenTimeout = 30 * time.Second

// maxErrorBody caps what a failing answer can push into the log, like httpclient does with a body.
const maxErrorBody = 4 << 10

type FirebaseOptions struct {
	Bucket string
	// The service account file of ADR-0069, regla 3. Ignored when EmulatorHost is set: the emulator takes no credential.
	CredentialsFile string
	// The base URL of a fake-gcs-server, which config allows only in development and test. The variable that carries it,
	// STORAGE_EMULATOR_HOST, is the one Google's own clients honour, so the worker's Python client reads the same one.
	EmulatorHost string
	// A whole upload of up to 100 MB has to fit in it, so it is far longer than any other timeout of the core.
	Timeout time.Duration
	// Replaces the client built from the credential, so a test needs neither a service account nor a socket to Google.
	HTTP   *http.Client
	Logger *slog.Logger
}

// Firebase writes and deletes objects in one bucket. It never reads one: reading is the worker's (ADR-0019).
type Firebase struct {
	base    string
	bucket  string
	timeout time.Duration
	http    *http.Client
	log     *slog.Logger
}

// NewFirebase reads the credential at boot, so a missing or broken file stops the process instead of the first upload.
func NewFirebase(opts FirebaseOptions) (*Firebase, error) {
	if strings.TrimSpace(opts.Bucket) == "" {
		return nil, fmt.Errorf("storage: no bucket to write to")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	f := &Firebase{base: defaultBaseURL, bucket: opts.Bucket, timeout: opts.Timeout, http: opts.HTTP, log: opts.Logger}
	if opts.EmulatorHost != "" {
		f.base = emulatorURL(opts.EmulatorHost)
		if f.http == nil {
			f.http = &http.Client{}
		}
		return f, nil
	}
	if f.http != nil {
		return f, nil
	}

	raw, err := os.ReadFile(opts.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("storage: cannot read the service account file: %w", err)
	}
	// Background and not the boot's context, because the token source keeps it to refresh the token for as long as the process
	// lives; and with a client of its own, because without one the exchange runs on http.DefaultClient, which never times out.
	// A token endpoint that accepts and never answers would then hold every upload behind the token source's lock, and the boot.
	tokens := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: tokenTimeout})
	creds, err := google.CredentialsFromJSON(tokens, raw, readWriteScope)
	if err != nil {
		return nil, fmt.Errorf("storage: the service account file is not a credential: %w", err)
	}
	// Asked once here, so a credential Google refuses stops the boot like a missing file does.
	if _, err := creds.TokenSource.Token(); err != nil {
		return nil, fmt.Errorf("storage: Google refused the service account: %w", err)
	}
	f.http = oauth2.NewClient(context.Background(), creds.TokenSource)
	return f, nil
}

// Put writes the object whole, in one request: the core already holds the encrypted file in memory (ADR-0070).
func (f *Firebase) Put(ctx context.Context, key string, data []byte) error {
	endpoint := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s",
		f.base, url.PathEscape(f.bucket), url.QueryEscape(key))
	_, err := f.do(ctx, http.MethodPost, endpoint, data)
	return err
}

// Delete answers nil for an object that is not there: it is called to clean an orphan, and a missing one is already clean.
func (f *Firebase) Delete(ctx context.Context, key string) error {
	endpoint := fmt.Sprintf("%s/storage/v1/b/%s/o/%s", f.base, url.PathEscape(f.bucket), url.PathEscape(key))
	status, err := f.do(ctx, http.MethodDelete, endpoint, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (f *Firebase) do(ctx context.Context, method, endpoint string, body []byte) (int, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, fmt.Errorf("storage: build %s: %w", method, err)
	}
	if body != nil {
		// Ciphertext, so nothing about the content type of the PDF is worth telling the store (ADR-0064, regla 1).
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	res, err := f.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("storage: %s: %w", method, err)
	}
	defer func() { _ = res.Body.Close() }()

	payload, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		// The same type the other clients of the core return, so a caller can tell a 4xx from a 5xx the same way.
		return res.StatusCode, &httpclient.StatusError{Status: res.StatusCode, Body: string(payload)}
	}
	return res.StatusCode, nil
}

// emulatorURL accepts the variable with or without a scheme, as Google's clients do: "storage:4443" means plain http.
func emulatorURL(host string) string {
	host = strings.TrimSuffix(strings.TrimSpace(host), "/")
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}
	return "http://" + host
}
