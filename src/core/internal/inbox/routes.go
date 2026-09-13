package inbox

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The three roles enter both routes, and what each one reaches is decided inside: refusing the listing to an operator would take
// away the inbox Anexo B gives it, and refusing it the answer would leave nobody to take the pool (ADR-0004).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/conversations", Roles: enums.OperatorRoleValues, Handler: c.list},
		// The same rule for every role, applied by the service: the conversation is the caller's or the pool's (Anexo B, decision 5).
		{Method: http.MethodPost, Path: "/conversations/:id/messages", Roles: enums.OperatorRoleValues, Handler: c.send},
	}
}
