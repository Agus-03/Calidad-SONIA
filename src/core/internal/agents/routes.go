package agents

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Written once so the five routes of one agent cannot drift apart.
const routeBase = "/ai-agents"

// The admin alone, reading included: F17 places the whole management "desde el panel admin" and Anexo B gives it to the Admin, and
// no other role has a screen that needs an agent's configuration (ADR-0038).
func (c *Controller) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: routeBase, Roles: admin, Handler: c.list},
		{Method: http.MethodPost, Path: routeBase, Roles: admin, Handler: c.create},
		{Method: http.MethodGet, Path: routeBase + "/:id", Roles: admin, Handler: c.show},
		{Method: http.MethodPatch, Path: routeBase + "/:id", Roles: admin, Handler: c.update},
		{Method: http.MethodDelete, Path: routeBase + "/:id", Roles: admin, Handler: c.remove},
	}
}
