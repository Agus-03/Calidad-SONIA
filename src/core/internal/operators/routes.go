package operators

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Routes names no role on purpose: whoever is logging in is not an actor yet, so the router mounts this outside /api/v1.
func (c *LoginController) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/auth/login", Handler: c.login},
	}
}

// The contract's own list and not three words: any authenticated operator may ask who they are, so a role added later is included by itself (F18).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{
			Method:  http.MethodGet,
			Path:    "/me",
			Roles:   enums.OperatorRoleValues,
			Handler: c.me,
		},
	}
}

// The supervisor reads the team to reassign (F26), the admin writes it (Anexo B, F18), and all change their own password (ADR-0062).
func (c *RosterController) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}
	readers := []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/operators", Roles: readers, Handler: c.list},
		{Method: http.MethodPost, Path: "/operators", Roles: admin, Handler: c.create},
		{Method: http.MethodPatch, Path: "/operators/:id", Roles: admin, Handler: c.update},
		{Method: http.MethodDelete, Path: "/operators/:id", Roles: admin, Handler: c.remove},
		{Method: http.MethodPut, Path: "/operators/:id/password", Roles: admin, Handler: c.resetPassword},
		{Method: http.MethodPut, Path: "/me/password", Roles: enums.OperatorRoleValues, Handler: c.changePassword},
	}
}
