package labels

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Reading takes the contract's own list because F19 gives the admin the definition of the catalogue and not its reading (ADR-0032, regla 5).
// Writing is the admin alone, which is the one role Anexo A names in this area (F19, ADR-0032, regla 13).
func (c *Controller) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/labels", Roles: enums.OperatorRoleValues, Handler: c.list},
		{Method: http.MethodPost, Path: "/labels", Roles: admin, Handler: c.create},
		{Method: http.MethodPatch, Path: "/labels/:id", Roles: admin, Handler: c.update},
		{Method: http.MethodDelete, Path: "/labels/:id", Roles: admin, Handler: c.remove},
	}
}
