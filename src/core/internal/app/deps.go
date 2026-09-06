package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/conversations"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/config"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/db"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
	"github.com/mateo-velasquez/sonia/src/core/internal/whatsapp"
)

// wiring is everything the router needs from the bounded contexts.
type wiring struct {
	controllers []soniahttp.Controller
	actorLookup soniahttp.ActorLookup

	// What Meta calls: outside /api/v1 because an external callback is not a resource of the operator API.
	publicControllers []soniahttp.Controller
}

// wire builds the object graph, repository then service then controller: it is the only part that grows with each context.
func wire(cfg config.Config, pool db.Pool, log *slog.Logger) wiring {
	aggregate := conversations.NewService(conversations.NewRepository(pool), log)
	// The turn is nil until Fase 1 wires turn/: the message is stored and, for now, nobody answers it.
	channel := whatsapp.NewController(whatsapp.NewService(whatsapp.NewRepository(pool), aggregate, log), nil, log)

	return wiring{
		controllers:       nil,
		actorLookup:       denyEveryone,
		publicControllers: []soniahttp.Controller{channel},
	}
}

// denyEveryone stands in until operators exists: Fase 0 mounts no endpoint under /api/v1, so no request should get past authentication.
func denyEveryone(context.Context, uuid.UUID) (soniahttp.Actor, error) {
	return soniahttp.Actor{}, soniahttp.Unauthorized("authentication_unavailable", "operator authentication is not wired yet")
}
