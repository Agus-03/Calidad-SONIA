package sales

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles enter every route, and the service decides: the vendor its own, supervisor and admin any of the account (ADR-0059).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/conversations/:id/sales", Roles: enums.OperatorRoleValues, Handler: c.mark},
		{Method: http.MethodGet, Path: "/conversations/:id/sales", Roles: enums.OperatorRoleValues, Handler: c.ofConversation},
		{Method: http.MethodPatch, Path: "/sales/:id", Roles: enums.OperatorRoleValues, Handler: c.update},
		{Method: http.MethodDelete, Path: "/sales/:id", Roles: enums.OperatorRoleValues, Handler: c.remove},
	}
}
