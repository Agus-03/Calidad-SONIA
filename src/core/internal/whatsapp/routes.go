package whatsapp

import (
	"net/http"

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
