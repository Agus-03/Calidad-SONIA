package http

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// Controller is a bounded context that declares its endpoints without mounting them: gin is touched here and nowhere else.
type Controller interface {
	Routes() []Route
}

// RouterOptions is what NewRouter needs from the bootstrap.
type RouterOptions struct {
	Logger      *slog.Logger
	ActorLookup ActorLookup
	Controllers []Controller
	Ready       func() error
}

// NewRouter puts /health outside authentication, every context under /api/v1, and panics on a route that declares no roles.
func NewRouter(opts RouterOptions) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	engine := gin.New()
	engine.Use(RequestID(), AccessLog(opts.Logger), Recovery(opts.Logger))

	engine.GET("/health", func(c *gin.Context) {
		if opts.Ready != nil {
			if err := opts.Ready(); err != nil {
				Fail(c, opts.Logger, Unavailable("not_ready", "dependencies unavailable").WithCause(err))
				return
			}
		}
		OK(c, gin.H{"status": "ok"})
	})

	api := engine.Group("/api/v1", Authenticate(opts.ActorLookup))
	for _, controller := range opts.Controllers {
		for _, route := range controller.Routes() {
			if len(route.Roles) == 0 {
				panic(fmt.Sprintf("http: route %s %s declares no roles", route.Method, route.Path))
			}
			api.Handle(route.Method, route.Path, RequireRole(route.Roles...), route.Handler)
		}
	}

	return engine
}
