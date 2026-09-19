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

// Under the agent they belong to, and for the admin alone for the same reason: the Admin of Anexo B "configura los componentes del
// prompt de los agentes" (ADR-0039).
func (c *ComponentsController) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}
	components := routeBase + "/:id/components"

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: components, Roles: admin, Handler: c.list},
		{Method: http.MethodPost, Path: components, Roles: admin, Handler: c.create},
		{Method: http.MethodPatch, Path: components + "/:componentId", Roles: admin, Handler: c.update},
		{Method: http.MethodDelete, Path: components + "/:componentId", Roles: admin, Handler: c.remove},
		{Method: http.MethodGet, Path: components + "/:componentId/versions", Roles: admin, Handler: c.versions},
	}
}

// A POST that writes nothing, because the drafts travel in the body: F03 shows the prompt "a medida que edita", before any save.
// The admin alone, for the reason the components are (ADR-0040).
func (c *PreviewController) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodPost, Path: routeBase + "/:id/preview", Roles: admin, Handler: c.preview},
	}
}
