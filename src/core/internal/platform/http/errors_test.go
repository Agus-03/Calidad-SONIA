package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestConstructorsCarryTheStatusTheirNameClaims protects the mapping the frontend switches on: a wrong status is a wrong branch there.
func TestConstructorsCarryTheStatusTheirNameClaims(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want int
	}{
		{"bad request", BadRequest("invalid_body", "m"), http.StatusBadRequest},
		{"unauthorized", Unauthorized("missing_credentials", "m"), http.StatusUnauthorized},
		{"forbidden", Forbidden("insufficient_role", "m"), http.StatusForbidden},
		{"not found", NotFound("agent_not_found", "m"), http.StatusNotFound},
		{"conflict", Conflict("already_exists", "m"), http.StatusConflict},
		{"unavailable", Unavailable("not_ready", "m"), http.StatusServiceUnavailable},
		{"internal", Internal(errors.New("boom")), http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Status != tc.want {
				t.Fatalf("status %d, want %d", tc.err.Status, tc.want)
			}
			if tc.err.Code == "" {
				t.Fatalf("constructor produced an empty code, which the frontend cannot switch on")
			}
		})
	}
}

// TestTheCauseNeverReachesTheBody protects ADR-0006 territory: the reason belongs in the log, never in what the client reads.
func TestTheCauseNeverReachesTheBody(t *testing.T) {
	secret := "pq: password authentication failed for user sonia"

	var logged bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logged, nil))

	gin.SetMode(gin.ReleaseMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/agents", nil)

	Fail(c, log, Internal(errors.New(secret)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("the cause reached the response body: %s", rec.Body.String())
	}
	if !strings.Contains(logged.String(), secret) {
		t.Fatalf("the cause never reached the log, so the 500 cannot be diagnosed")
	}
}

// TestFailTurnsAnUnknownErrorIntoAnInternal protects the boundary: a domain error that nobody mapped must not leak its text.
func TestFailTurnsAnUnknownErrorIntoAnInternal(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/agents", nil)

	Fail(c, nil, errors.New("conversations: repository exploded"))

	var body struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not the error envelope: %v", err)
	}
	if body.Error.Code != "internal_error" {
		t.Fatalf("code %q, want internal_error", body.Error.Code)
	}
	if strings.Contains(rec.Body.String(), "exploded") {
		t.Fatalf("an unmapped error leaked its text: %s", rec.Body.String())
	}
}

// TestErrorUnwrapsToItsCause protects errors.Is at the call site: a wrapped sentinel has to stay findable.
func TestErrorUnwrapsToItsCause(t *testing.T) {
	sentinel := errors.New("whatsapp: inbox not found")
	wrapped := NotFound("inbox_not_found", "no inbox").WithCause(sentinel)

	if !errors.Is(wrapped, sentinel) {
		t.Fatalf("errors.Is could not reach the cause through the api error")
	}
	if !strings.Contains(wrapped.Error(), "inbox_not_found") {
		t.Fatalf("Error() %q does not name its code", wrapped.Error())
	}
}

// TestWithDetailsTravelsToTheClient protects field level validation: without details a 400 cannot say which field failed.
func TestWithDetailsTravelsToTheClient(t *testing.T) {
	err := BadRequest("invalid_body", "m").WithDetails(map[string]string{"name": "required"})

	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	if !strings.Contains(string(encoded), `"name":"required"`) {
		t.Fatalf("details did not survive encoding: %s", encoded)
	}
	if strings.Contains(string(encoded), "Status") {
		t.Fatalf("the status is duplicated inside the body: %s", encoded)
	}
}

// TestSuccessHelpersWriteTheirStatus protects the rule that nothing answers with a hand written c.JSON.
func TestSuccessHelpersWriteTheirStatus(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)

	cases := []struct {
		name  string
		write func(*gin.Context)
		want  int
	}{
		{"ok", func(c *gin.Context) { OK(c, gin.H{"status": "ok"}) }, http.StatusOK},
		{"created", func(c *gin.Context) { Created(c, gin.H{"id": "1"}) }, http.StatusCreated},
		{"no content", NoContent, http.StatusNoContent},
		{"text", func(c *gin.Context) { Text(c, "challenge") }, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

			tc.write(c)
			// gin holds the status until the handler chain ends, and calling the helper on its own never reaches that point.
			c.Writer.WriteHeaderNow()

			if rec.Code != tc.want {
				t.Fatalf("status %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
