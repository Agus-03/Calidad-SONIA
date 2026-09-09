package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type stubController struct{ routes []Route }

func (s stubController) Routes() []Route { return s.routes }

// TestNewRouterPanicsOnARouteWithNoRoles protects the boot: a process that does not start beats one that publishes an open endpoint.
func TestNewRouterPanicsOnARouteWithNoRoles(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("NewRouter accepted a route with no roles, want a panic")
		}
	}()

	NewRouter(RouterOptions{
		Controllers: []Controller{stubController{routes: []Route{{
			Method:  http.MethodGet,
			Path:    "/agents",
			Handler: func(c *gin.Context) { OK(c, gin.H{}) },
		}}}},
	})
}

// TestNewRouterPanicsOnAPublicRouteWithRoles is the mirror guard: a public route naming a role hides which zone it is really in.
func TestNewRouterPanicsOnAPublicRouteWithRoles(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatalf("NewRouter accepted a public route with roles, want a panic")
		}
	}()

	NewRouter(RouterOptions{
		PublicControllers: []Controller{stubController{routes: []Route{{
			Method:  http.MethodGet,
			Path:    "/webhooks/whatsapp",
			Roles:   []string{RoleAdmin},
			Handler: func(c *gin.Context) { OK(c, gin.H{}) },
		}}}},
	})
}

// TestPublicRoutesSkipAuthentication protects the decision: an external callback carries no bearer token and must still get through.
func TestPublicRoutesSkipAuthentication(t *testing.T) {
	engine := NewRouter(RouterOptions{
		TokenVerifier: func(string) (uuid.UUID, error) {
			return uuid.Nil, Unauthorized("unreachable", "a public route must never verify a token")
		},
		ActorLookup: func(context.Context, uuid.UUID) (Actor, error) {
			return Actor{}, Unauthorized("unreachable", "a public route must never consult the lookup")
		},
		PublicControllers: []Controller{stubController{routes: []Route{{
			Method:  http.MethodGet,
			Path:    "/webhooks/whatsapp",
			Handler: func(c *gin.Context) { Text(c, "challenge") },
		}}}},
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/webhooks/whatsapp", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "challenge" {
		t.Fatalf("public route answered %d %q, want 200 \"challenge\"", rec.Code, rec.Body.String())
	}
}

// TestRouterAuthorizesBeforeTheHandler protects Anexo B: a role checked inside the service has already let the request in.
func TestRouterAuthorizesBeforeTheHandler(t *testing.T) {
	admin, operator := uuid.New(), uuid.New()

	var reached bool
	engine := NewRouter(RouterOptions{
		// A token that is just the id, because what this test asserts is the order of the two guards and not the signature of a JWT.
		TokenVerifier: func(token string) (uuid.UUID, error) {
			operatorID, err := uuid.Parse(token)
			if err != nil {
				return uuid.Nil, Unauthorized("invalid_credentials", "the session token is not valid")
			}
			return operatorID, nil
		},
		ActorLookup: func(_ context.Context, operatorID uuid.UUID) (Actor, error) {
			if operatorID == admin {
				return Actor{OperatorID: admin, Role: RoleAdmin}, nil
			}
			return Actor{OperatorID: operatorID, Role: RoleOperator}, nil
		},
		Controllers: []Controller{stubController{routes: []Route{{
			Method:  http.MethodGet,
			Path:    "/agents",
			Roles:   []string{RoleAdmin},
			Handler: func(c *gin.Context) { reached = true; OK(c, gin.H{}) },
		}}}},
	})

	cases := []struct {
		name       string
		authorize  string
		wantStatus int
		wantReach  bool
	}{
		{
			name:       "a request with no identity never reaches the handler",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a token the verifier refuses never reaches the lookup",
			authorize:  "Bearer no-soy-un-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "an identity sent in another scheme is no identity at all",
			authorize:  admin.String(),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "an operator is rejected on an admin route before the handler runs",
			authorize:  "Bearer " + operator.String(),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin gets through",
			authorize:  "Bearer " + admin.String(),
			wantStatus: http.StatusOK,
			wantReach:  true,
		},
		{
			name:       "the scheme is read without regard to case, as the rfc defines it",
			authorize:  "bearer " + admin.String(),
			wantStatus: http.StatusOK,
			wantReach:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false

			req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
			if tc.authorize != "" {
				req.Header.Set("Authorization", tc.authorize)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != tc.wantReach {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantReach)
			}
		})
	}
}
