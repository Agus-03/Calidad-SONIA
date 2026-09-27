package inbox

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// StreamController hands out the ticket, behind the bearer like any route of the operator API (ADR-0051).
type StreamController struct {
	hub *Hub
	log *slog.Logger
}

func NewStreamController(hub *Hub, log *slog.Logger) *StreamController {
	if log == nil {
		log = slog.Default()
	}
	return &StreamController{hub: hub, log: log}
}

// The three roles, as the listing: each hears what its inbox shows, and the hub decides that per connection (ADR-0035, regla 1).
func (c *StreamController) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodPost, Path: "/inbox/stream-ticket", Roles: enums.OperatorRoleValues, Handler: c.ticket},
	}
}

type ticketResponse struct {
	Ticket    string `json:"ticket"`
	ExpiresAt string `json:"expires_at"`
}

func (c *StreamController) ticket(gc *gin.Context) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return
	}
	token, expiresAt, err := c.hub.Issue(actor.OperatorID)
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.Internal(err))
		return
	}
	soniahttp.Created(gc, ticketResponse{Ticket: token, ExpiresAt: expiresAt.UTC().Format(time.RFC3339)})
}

// StreamGate is the socket itself. Public for the router because the browser cannot send the bearer when it opens a WebSocket:
// the ticket is the credential, and the operator is read again from the database before anything is sent (ADR-0051).
type StreamGate struct {
	hub    *Hub
	lookup soniahttp.ActorLookup
	log    *slog.Logger
}

func NewStreamGate(hub *Hub, lookup soniahttp.ActorLookup, log *slog.Logger) *StreamGate {
	if log == nil {
		log = slog.Default()
	}
	return &StreamGate{hub: hub, lookup: lookup, log: log}
}

// Outside /api/v1 because a public route cannot name roles; the reach is still the actor's, read below.
func (g *StreamGate) Routes() []soniahttp.Route {
	return []soniahttp.Route{
		{Method: http.MethodGet, Path: "/inbox/stream", Handler: g.stream},
	}
}

func (g *StreamGate) stream(gc *gin.Context) {
	operatorID, err := g.hub.Redeem(gc.Query("ticket"))
	if errors.Is(err, ErrInvalidTicket) {
		soniahttp.Fail(gc, g.log, soniahttp.Unauthorized("invalid_ticket", "the stream ticket is unknown, used or expired: ask for a new one").WithCause(err))
		return
	}
	if err != nil {
		soniahttp.Fail(gc, g.log, err)
		return
	}
	// Read again and not taken from the ticket: an operator disabled in the thirty seconds between the two is refused (ADR-0030, regla 2).
	actor, err := g.lookup(gc.Request.Context(), operatorID)
	if err != nil {
		soniahttp.Fail(gc, g.log, err)
		return
	}

	// The origin is not checked because the ticket is the credential: a page of another site cannot get one without the operator's
	// bearer, which is what an origin check would protect (ADR-0051).
	conn, err := websocket.Accept(gc.Writer, gc.Request, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		// Accept already answered the browser; what is left is the trace.
		g.log.Warn("inbox stream not opened", slog.String("operator_id", actor.OperatorID.String()), slog.String("cause", err.Error()))
		return
	}
	g.hub.Serve(gc.Request.Context(), conn, Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID, Role: actor.Role})
}
