package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// wiring is everything the router needs from the bounded contexts.
type wiring struct {
	controllers []soniahttp.Controller
	actorLookup soniahttp.ActorLookup
}

// wire builds the object graph, repository then service then controller: it is the only part that grows with each context.
func wire(cfg config.Config, pool db.Pool, log *slog.Logger) wiring {
	return wiring{
		controllers: nil,
		actorLookup: denyEveryone,
	}
}

// denyEveryone stands in until operators exists: Fase 0 mounts no endpoint under /api/v1, so no request should get past authentication.
func denyEveryone(context.Context, uuid.UUID) (soniahttp.Actor, error) {
	return soniahttp.Actor{}, soniahttp.Unauthorized("authentication_unavailable", "operator authentication is not wired yet")
}
