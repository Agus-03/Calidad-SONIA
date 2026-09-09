package http

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Actor is who is making the request, with the role typed: the three words are generated from Anexo B and not retyped here (C01).
type Actor struct {
	OperatorID uuid.UUID
	Role       enums.OperatorRole
}

// TokenVerifier answers who the bearer is and returns the error the caller should see, so this package never learns the token is a JWT (ADR-0030).
type TokenVerifier func(token string) (uuid.UUID, error)

// ActorLookup reads the row the token deliberately does not carry: the role and whether the operator is still active (ADR-0030, regla 2).
type ActorLookup func(ctx context.Context, operatorID uuid.UUID) (Actor, error)

const actorKey = "sonia.actor"

// Authenticate takes the identity from the signed token and the permissions from the database, in that order.
func Authenticate(verify TokenVerifier, lookup ActorLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			Fail(c, nil, Unauthorized("missing_credentials", "no bearer token in the request"))
			return
		}
		operatorID, err := verify(raw)
		if err != nil {
			Fail(c, nil, err)
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

// The scheme is matched without regard to case because RFC 7235 defines it as case-insensitive and browsers do send "bearer".
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
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
func RequireRole(allowed ...enums.OperatorRole) gin.HandlerFunc {
	permitted := make(map[enums.OperatorRole]struct{}, len(allowed))
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
