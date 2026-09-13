package inbox

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Written once so the four routes of one conversation cannot drift apart.
const routeBase = "/conversations"

// The three roles enter the listing, the thread and the answer, and what each one reaches is decided inside: refusing the listing to
// an operator would take away the inbox Anexo B gives it, and refusing it the answer would leave nobody to take the pool (ADR-0004).
func (c *Controller) Routes() []soniahttp.Route {
	aboveOperator := []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: routeBase, Roles: enums.OperatorRoleValues, Handler: c.list},
		// The reach of the listing, one conversation at a time: what the operator could not see there it cannot read here (ADR-0035, regla 2).
		{Method: http.MethodGet, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.thread},
		// The same rule for every role, applied by the service: the conversation is the caller's or the pool's (ADR-0035, regla 5).
		{Method: http.MethodPost, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.send},
		// The two roles above the vendors, over every conversation of the account because both see the whole team (ADR-0035, regla 10).
		// It departs from F27 and Anexo B, which let the basic operator turn the AI agent of a chat on and off.
		{Method: http.MethodPatch, Path: routeBase + "/:id/mode", Roles: aboveOperator, Handler: c.setMode},
	}
}
