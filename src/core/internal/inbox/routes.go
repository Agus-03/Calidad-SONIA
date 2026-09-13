package inbox

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles enter the listing, and what each one sees is narrowed inside by the scope: refusing the route to an operator would
// take away the inbox Anexo B gives it, which is its own conversations and the pool (ADR-0004).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/conversations", Roles: enums.OperatorRoleValues, Handler: c.list},
	}
}
