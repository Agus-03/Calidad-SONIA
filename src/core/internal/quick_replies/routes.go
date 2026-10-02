package quickreplies

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles read, because F09 has the operators pick from the chat; only the admin writes, because F09 says the admin loads them.
func (c *Controller) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/quick-replies", Roles: enums.OperatorRoleValues, Handler: c.list},
		{Method: http.MethodPost, Path: "/quick-replies", Roles: admin, Handler: c.create},
		{Method: http.MethodPatch, Path: "/quick-replies/:id", Roles: admin, Handler: c.update},
		{Method: http.MethodDelete, Path: "/quick-replies/:id", Roles: admin, Handler: c.remove},
	}
}
