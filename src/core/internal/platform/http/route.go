package http

import "github.com/gin-gonic/gin"

// Route is one endpoint declared as data: method, path, roles and handler are one decision and travel together.
type Route struct {
	Method  string
	Path    string
	Roles   []string
	Handler gin.HandlerFunc
}
