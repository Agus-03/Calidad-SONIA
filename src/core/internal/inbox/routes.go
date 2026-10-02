package inbox

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Written once so the thirteen routes of one conversation cannot drift apart.
const routeBase = "/conversations"

// The three roles enter every route, and what each one reaches is decided inside: refusing the listing to an operator would take away
// the inbox Anexo B gives it, and refusing it the answer would leave nobody to take the pool (ADR-0004).
func (c *Controller) Routes() []soniahttp.Route {
	// Anexo B gives reassigning to the Super Agent and denies it to the operator, and the group kept it so (ADR-0056).
	aboveOperator := []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: routeBase, Roles: enums.OperatorRoleValues, Handler: c.list},
		// The reach of the listing, one conversation at a time: what the operator could not see there it cannot read here (ADR-0035, regla 2).
		{Method: http.MethodGet, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.thread},
		// The same rule for every role, applied by the service: the conversation is the caller's or the pool's (ADR-0035, regla 5).
		{Method: http.MethodPost, Path: routeBase + "/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.send},
		// The draft of suggestion mode, with the reach of the thread: what the Operator cannot read it cannot copy (ADR-0046).
		{Method: http.MethodGet, Path: routeBase + "/:id/suggestion", Roles: enums.OperatorRoleValues, Handler: c.suggestion},
		// The operator switches the bot of what it holds, as F27 and Anexo B give it; supervisor and admin, of any conversation of the
		// account. The service tells them apart (ADR-0037, regla 4).
		{Method: http.MethodPatch, Path: routeBase + "/:id/mode", Roles: enums.OperatorRoleValues, Handler: c.setMode},
		// F25 names no role, so the star has the reach of the thread for all three, which the service decides (ADR-0035, regla 2).
		{Method: http.MethodPatch, Path: routeBase + "/:id/starred", Roles: enums.OperatorRoleValues, Handler: c.setStarred},
		// Who may answer is who may clear the unread: the holder, or anybody for the pool, which the service decides (ADR-0052).
		{Method: http.MethodPost, Path: routeBase + "/:id/read", Roles: enums.OperatorRoleValues, Handler: c.markRead},
		// The vendor closes what it holds and the two roles above any, as with the bot, which the service decides (ADR-0054).
		{Method: http.MethodPost, Path: routeBase + "/:id/resolve", Roles: enums.OperatorRoleValues, Handler: c.resolve},
		// F13: only the vendor scores, and only once it is resolved; the three roles enter and the service tells them apart (ADR-0055).
		{Method: http.MethodPut, Path: routeBase + "/:id/score", Roles: enums.OperatorRoleValues, Handler: c.setScore},
		{Method: http.MethodPost, Path: routeBase + "/:id/reassign", Roles: aboveOperator, Handler: c.reassign},
		// The absence of a vendor: every open conversation of one goes to another in one request (ADR-0056).
		{Method: http.MethodPost, Path: "/inbox/handover", Roles: aboveOperator, Handler: c.handOver},
		// The history has the reach of the thread, which the service decides for the three roles (ADR-0056).
		{Method: http.MethodGet, Path: routeBase + "/:id/assignments", Roles: enums.OperatorRoleValues, Handler: c.assignments},
		// F24 with the reach of the thread for the three roles, which the service decides; the catalogue stays the admin's (ADR-0057).
		{Method: http.MethodPost, Path: routeBase + "/:id/labels", Roles: enums.OperatorRoleValues, Handler: c.assignLabel},
		{Method: http.MethodDelete, Path: routeBase + "/:id/labels/:label_id", Roles: enums.OperatorRoleValues, Handler: c.unassignLabel},
	}
}
