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

	// TokenVerifier is separate from ActorLookup because one answers who the caller claims to be and the other what they may do.
	TokenVerifier TokenVerifier

	// PublicControllers hang outside /api/v1: an external callback has no operator, so no role can describe it.
	PublicControllers []Controller
}

// NewRouter puts /health and the public controllers outside authentication, every context under /api/v1, and panics on a route whose roles contradict its zone.
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

	// The guard is the mirror of the authenticated one: a public route naming a role is a contradiction, and it is cheaper to hear it at boot.
	for _, controller := range opts.PublicControllers {
		for _, route := range controller.Routes() {
			if len(route.Roles) > 0 {
				panic(fmt.Sprintf("http: public route %s %s declares roles", route.Method, route.Path))
			}
			engine.Handle(route.Method, route.Path, route.Handler)
		}
	}

	api := engine.Group("/api/v1", Authenticate(opts.TokenVerifier, opts.ActorLookup))
	for _, controller := range opts.Controllers {
		for _, route := range controller.Routes() {
			if len(route.Roles) == 0 {
				panic(fmt.Sprintf("http: route %s %s declares no roles", route.Method, route.Path))
			}
			// The type alone admits a typo, so the generated contract is what says the word exists: a misspelled role would only ever 403 (C01).
			for _, role := range route.Roles {
				if !role.IsValid() {
					panic(fmt.Sprintf("http: route %s %s declares role %q, which Anexo B does not", route.Method, route.Path, role))
				}
			}
			api.Handle(route.Method, route.Path, RequireRole(route.Roles...), route.Handler)
		}
	}

	return engine
}
