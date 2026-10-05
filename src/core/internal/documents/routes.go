package documents

import (
	"net/http"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// Under the agent, with the same :id agents/ uses, because gin needs one name per segment and the base belongs to the agent.
const agentDocuments = "/ai-agents/:id/documents"

// The admin alone, reading included: the knowledge base is part of the agent's configuration, and Anexo B gives the agents to
// the Admin, as ADR-0038 reads it for agents/.
func (c *Controller) Routes() []soniahttp.Route {
	admin := []enums.OperatorRole{enums.OperatorRoleAdmin}

	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/documents/notice", Roles: admin, Handler: c.notice},
		{Method: http.MethodGet, Path: agentDocuments, Roles: admin, Handler: c.list},
		{Method: http.MethodPost, Path: agentDocuments, Roles: admin, Handler: c.upload},
		{Method: http.MethodDelete, Path: agentDocuments + "/:documentId", Roles: admin, Handler: c.remove},
	}
}
