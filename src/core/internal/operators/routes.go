package operators

import (
	"net/http"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Routes names no role on purpose: whoever is logging in is not an actor yet, so the router mounts this outside /api/v1.
func (c *LoginController) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/auth/login", Handler: c.login},
	}
}

// Routes lists the three roles because any authenticated operator may ask who they are: the finer split is Fase 11 (F18).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{
			Method:  http.MethodGet,
			Path:    "/me",
			Roles:   []string{soniahttp.RoleOperator, soniahttp.RoleSupervisor, soniahttp.RoleAdmin},
			Handler: c.me,
		},
	}
}
