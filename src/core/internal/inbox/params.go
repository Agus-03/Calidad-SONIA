package inbox

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

func limitFrom(gc *gin.Context) (int, error) {
	raw := gc.Query("limit")
	if raw == "" {
		return defaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, soniahttp.BadRequest("invalid_limit", "limit must be a positive whole number").WithCause(err)
	}
	// Clamped and not refused, the same as per_page in contacts/: asking for too many is not a mistake worth failing the page over
	// (ADR-0036, regla 8).
	if n > maxLimit {
		n = maxLimit
	}
	return n, nil
}

// filterFrom reads the filters of F23 from the query. A flag is on only with "true", and any other value is refused and not read as
// off: a frontend that sent "1" would draw the whole inbox as the filtered one (ADR-0050).
func filterFrom(gc *gin.Context) (Filter, error) {
	var f Filter
	for _, flag := range []struct {
		name string
		into *bool
	}{{"unread", &f.Unread}, {"assigned", &f.Assigned}, {"starred", &f.Starred}, {"reminder", &f.WithReminder}} {
		switch raw, present := gc.GetQuery(flag.name); {
		case !present:
		case raw == "true":
			*flag.into = true
		default:
			return Filter{}, soniahttp.BadRequest("invalid_filter", flag.name+" only takes true: leave it out to not filter by it")
		}
	}

	if raw, present := gc.GetQuery("status"); present {
		status := enums.ConversationStatus(raw)
		if !status.IsValid() {
			return Filter{}, soniahttp.BadRequest("invalid_status", "status is not one of active, waiting, resolved")
		}
		f.Status = &status
	}
	if raw, present := gc.GetQuery("label_id"); present {
		labelID, err := uuid.Parse(raw)
		if err != nil {
			return Filter{}, soniahttp.BadRequest("invalid_label_id", "the label id is not a uuid").WithCause(err)
		}
		f.LabelID = &labelID
	}
	return f, nil
}

// cursorFrom reads the token the previous page handed out. An absent one is the first page; a malformed one is refused, because
// guessing would answer the first page again and the frontend would draw it as the next (ADR-0036, regla 5).
func cursorFrom(gc *gin.Context) (*Cursor, error) {
	raw := gc.Query("cursor")
	if raw == "" {
		return nil, nil
	}
	cursor, err := decodeCursor(raw)
	if err != nil {
		return nil, soniahttp.BadRequest("invalid_cursor", "the cursor is not one this listing handed out").WithCause(err)
	}
	return &cursor, nil
}

var errMalformedCursor = errors.New("inbox: malformed cursor")

// The cursor is opaque to the frontend on purpose: what it carries is how the query continues, and a client that learned to build one
// would break the day the ordering changes (ADR-0036, regla 4). Nanoseconds so the instant survives the round trip exactly and the
// comparison stays strict.
func encodeCursor(c Cursor) string {
	raw := c.At.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(token string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, err
	}
	rawAt, rawID, found := strings.Cut(string(raw), "|")
	if !found {
		return Cursor{}, errMalformedCursor
	}
	at, err := time.Parse(time.RFC3339Nano, rawAt)
	if err != nil {
		return Cursor{}, err
	}
	rowID, err := uuid.Parse(rawID)
	if err != nil {
		return Cursor{}, err
	}
	return Cursor{At: at, ID: rowID}, nil
}
