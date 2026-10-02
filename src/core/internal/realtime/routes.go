package realtime

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles, as the listing: each hears what its inbox shows, and the hub decides that per connection (ADR-0035, regla 1).
func (c *StreamController) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/inbox/stream-ticket", Roles: enums.OperatorRoleValues, Handler: c.ticket},
	}
}

// Outside /api/v1 because a public route cannot name roles; the reach is still the actor's, read below.
func (g *StreamGate) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/inbox/stream", Handler: g.stream},
	}
}
