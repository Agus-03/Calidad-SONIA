package inbox

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Written once so the seven routes of one conversation cannot drift apart.
const routeBase = "/conversations"

// The three roles enter every route, and what each one reaches is decided inside: refusing the listing to an operator would take away
// the inbox Anexo B gives it, and refusing it the answer would leave nobody to take the pool (ADR-0004).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: routeBase, Roles: enums.OperatorRoleValues, Handler: c.list},
		// The reach of the listing, one conversation at a time: what the operator could not see there it cannot read here (ADR-0035, regla 2).
		{Method: http.MethodGet, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.thread},
		// The same rule for every role, applied by the service: the conversation is the caller's or the pool's (ADR-0035, regla 5).
		{Method: http.MethodPost, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.send},
		// The draft of suggestion mode, with the reach of the thread: what the Operator cannot read it cannot copy (ADR-0046).
		{Method: http.MethodGet, Path: routeBase + "/:id/suggestion", Roles: enums.OperatorRoleValues, Handler: c.suggestion},
		// F05 with the reach of the thread; the text of each component only for the admin, which the service decides (ADR-0049).
		{Method: http.MethodGet, Path: routeBase + "/:id/generations/:generation_id/components", Roles: enums.OperatorRoleValues, Handler: c.reasoning},
		// The operator switches the bot of what it holds, as F27 and Anexo B give it; supervisor and admin, of any conversation of the
		// account. The service tells them apart (ADR-0037, regla 4).
		{Method: http.MethodPatch, Path: routeBase + "/:id/mode", Roles: enums.OperatorRoleValues, Handler: c.setMode},
		// F25 names no role, so the star has the reach of the thread for all three, which the service decides (ADR-0035, regla 2).
		{Method: http.MethodPatch, Path: routeBase + "/:id/starred", Roles: enums.OperatorRoleValues, Handler: c.setStarred},
	}
}
