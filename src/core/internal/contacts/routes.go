package contacts

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The table of roles carries what a whole route can decide. What it cannot decide is who edits which field: the three roles
// enter the PATCH and the difference between them lives inside the handler, checked against writableBy (ADR-0032, reglas 8 y 14).
func (c *Controller) Routes() []soniahttp.Route {
	// The two roles ADR-0032 names for creating and for the four sensitive fields: an operator who created one could not read it back,
	// because it would have no conversation assigned yet (regla 11).
	aboveOperator := []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/contacts", Roles: enums.OperatorRoleValues, Handler: c.list},
		{Method: http.MethodGet, Path: "/contacts/:id", Roles: enums.OperatorRoleValues, Handler: c.show},
		{Method: http.MethodPost, Path: "/contacts", Roles: aboveOperator, Handler: c.create},
		{Method: http.MethodPatch, Path: "/contacts/:id", Roles: enums.OperatorRoleValues, Handler: c.update},
		{Method: http.MethodPut, Path: "/contacts/:id/sensitive", Roles: aboveOperator, Handler: c.writeSensitive},
		// Only the admin, because a contact taken down disappears from the book of the whole team and not just from the caller's (regla 12).
		{Method: http.MethodDelete, Path: "/contacts/:id", Roles: []enums.OperatorRole{enums.OperatorRoleAdmin}, Handler: c.remove},
	}
}
