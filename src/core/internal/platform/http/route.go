package http

import (
	"github.com/gin-gonic/gin"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// Route is one endpoint declared as data: method, path, roles and handler are one decision and travel together.
type Route struct {
	Method  string
	Path    string
	Roles   []enums.OperatorRole
	Handler gin.HandlerFunc
}
