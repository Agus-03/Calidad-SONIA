package contacts

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

func contactIDFrom(gc *gin.Context) (uuid.UUID, error) {
	contactID, err := uuid.Parse(gc.Param("id"))
	if err != nil {
		return uuid.Nil, soniahttp.BadRequest("invalid_id", "the contact id is not a uuid").WithCause(err)
	}
	return contactID, nil
}

// filterFrom reads the segment off the query string. label_id repeats instead of carrying a comma separated list, because a
// repeated parameter is what a form with an "add criterion" button produces and what every http library already parses (F33).
//
// match is the operator between them, and it is the seam the search of the application will grow from: today one group of labels
// under one operator, later a group of groups. What it will never be is implicit — a filter that guessed would answer a book
// nobody asked for.
func filterFrom(gc *gin.Context) (Filter, error) {
	raw := gc.QueryArray("label_id")
	if len(raw) > maxLabelFilters {
		return Filter{}, soniahttp.BadRequest("too_many_labels", "a segment takes at most 20 labels")
	}

	ids := make([]uuid.UUID, 0, len(raw))
	for _, one := range raw {
		labelID, err := uuid.Parse(one)
		if err != nil {
			return Filter{}, soniahttp.BadRequest("invalid_id", "the label id "+one+" is not a uuid").WithCause(err)
		}
		ids = append(ids, labelID)
	}

	// The operator travels as typed and is validated in one place, the service, so the handler cannot disagree with it.
	return Filter{LabelIDs: ids, Match: Match(gc.Query("match")), Search: gc.Query("search")}, nil
}

// A page past the end answers an empty list with the real total, because "there is nothing here" is not the same as "this does not exist".
func pagination(gc *gin.Context) (page, perPage int, err error) {
	page, err = positive(gc.Query("page"), 1)
	if err != nil {
		return 0, 0, soniahttp.BadRequest("invalid_page", "page must be a positive whole number").WithCause(err)
	}
	perPage, err = positive(gc.Query("per_page"), defaultPerPage)
	if err != nil {
		return 0, 0, soniahttp.BadRequest("invalid_per_page", "per_page must be a positive whole number").WithCause(err)
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}
	return page, perPage, nil
}

func positive(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, errors.New("contacts: not a positive whole number")
	}
	return n, nil
}
