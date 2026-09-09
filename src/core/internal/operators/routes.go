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
