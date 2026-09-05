package http

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Roles of Anexo B, renamed by C01.
const (
	RoleOperator   = "operator"
	RoleSupervisor = "supervisor"
	RoleAdmin      = "admin"
)

// Actor is who is making the request.
type Actor struct {
	OperatorID uuid.UUID
	Role       string
}

// ActorLookup is a seam, not a mechanism: no ADR defines authentication yet.
type ActorLookup func(ctx context.Context, operatorID uuid.UUID) (Actor, error)

const actorKey = "sonia.actor"

// Authenticate resolves the actor and puts it in the request context.
func Authenticate(lookup ActorLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader("X-Operator-Id"))
		if raw == "" {
			Fail(c, nil, Unauthorized("missing_credentials", "no operator identity in the request"))
			return
		}
		operatorID, err := uuid.Parse(raw)
		if err != nil {
			Fail(c, nil, Unauthorized("invalid_credentials", "operator identity is not a uuid"))
			return
		}
		actor, err := lookup(c.Request.Context(), operatorID)
		if err != nil {
			Fail(c, nil, err)
			return
		}
		c.Set(actorKey, actor)
		c.Next()
	}
}

// ActorFrom returns false if the route was mounted without Authenticate.
func ActorFrom(c *gin.Context) (Actor, bool) {
	v, ok := c.Get(actorKey)
	if !ok {
		return Actor{}, false
	}
	actor, ok := v.(Actor)
	return actor, ok
}

// RequireRole rejects a role outside the allowed set.
func RequireRole(allowed ...string) gin.HandlerFunc {
	permitted := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		permitted[r] = struct{}{}
	}
	return func(c *gin.Context) {
		actor, ok := ActorFrom(c)
		if !ok {
			Fail(c, nil, Unauthorized("missing_credentials", "no operator identity in the request"))
			return
		}
		if _, ok := permitted[actor.Role]; !ok {
			Fail(c, nil, Forbidden("insufficient_role", "this operation requires a different role"))
			return
		}
		c.Next()
	}
}
