package reasoning

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// F05 with the reach of the thread; the text of each component only for the admin, which the service decides (ADR-0049).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/conversations/:id/generations/:generation_id/components", Roles: enums.OperatorRoleValues, Handler: c.reasoning},
	}
}
