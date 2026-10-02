package reminders

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles enter every route, and what each reaches is the service's: the contact for the reads, the owner for the changes (ADR-0058).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/contacts/:id/reminders", Roles: enums.OperatorRoleValues, Handler: c.create},
		{Method: http.MethodGet, Path: "/contacts/:id/reminders", Roles: enums.OperatorRoleValues, Handler: c.ofContact},
		{Method: http.MethodGet, Path: "/reminders", Roles: enums.OperatorRoleValues, Handler: c.mine},
		{Method: http.MethodPatch, Path: "/reminders/:id", Roles: enums.OperatorRoleValues, Handler: c.update},
		{Method: http.MethodDelete, Path: "/reminders/:id", Roles: enums.OperatorRoleValues, Handler: c.remove},
	}
}
