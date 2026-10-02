package notes

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The notes have the reach of the thread, which the service asks inbox/ for the three roles (ADR-0056, ADR-0063).
func (c *Controller) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/conversations/:id/notes", Roles: enums.OperatorRoleValues, Handler: c.notes},
		{Method: http.MethodPost, Path: "/conversations/:id/notes", Roles: enums.OperatorRoleValues, Handler: c.addNote},
		{Method: http.MethodPatch, Path: "/conversations/:id/notes/:note_id", Roles: enums.OperatorRoleValues, Handler: c.editNote},
		{Method: http.MethodDelete, Path: "/conversations/:id/notes/:note_id", Roles: enums.OperatorRoleValues, Handler: c.deleteNote},
	}
}
