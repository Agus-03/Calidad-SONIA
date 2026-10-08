package whatsapp

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Written once so the verbs of the webhook cannot drift from the URL Meta was configured with.
const routeBase = "/webhooks/whatsapp"

// Routes names no role on purpose: Meta is not an operator, so the router mounts these outside /api/v1.
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: routeBase, Handler: c.verify},
		{Method: http.MethodPost, Path: routeBase, Handler: c.receive},
	}
}

// Written once so the two routes of the panel cannot drift apart.
const settingsBase = "/inboxes"

// The admin alone, reading included: which agent answers a number is the configuration of F17, and Anexo B gives it to the Admin
// (ADR-0038).
func (c *SettingsController) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: settingsBase, Roles: admin, Handler: c.list},
		// CAMBIADO POR TOBI
		{Method: http.MethodPost, Path: settingsBase, Roles: admin, Handler: c.create},
		{Method: http.MethodPatch, Path: settingsBase + "/:id", Roles: admin, Handler: c.setDefault},
	}
}
