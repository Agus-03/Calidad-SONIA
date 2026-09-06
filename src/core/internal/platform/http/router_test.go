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

// TestPublicRoutesSkipAuthentication protects the decision: an external callback carries no operator header and must still get through.
func TestPublicRoutesSkipAuthentication(t *testing.T) {
	engine := NewRouter(RouterOptions{
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
		operatorID string
		wantStatus int
		wantReach  bool
	}{
		{
			name:       "a request with no identity never reaches the handler",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "an operator is rejected on an admin route before the handler runs",
			operatorID: operator.String(),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin gets through",
			operatorID: admin.String(),
			wantStatus: http.StatusOK,
			wantReach:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false

			req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
			if tc.operatorID != "" {
				req.Header.Set("X-Operator-Id", tc.operatorID)
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
