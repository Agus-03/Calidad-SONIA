package contacts

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestAMalformedPageIsRefusedInsteadOfGuessed keeps a typo from silently becoming page one, which would look like contacts going missing.
func TestAMalformedPageIsRefusedInsteadOfGuessed(t *testing.T) {
	store, _ := seededContact()

	for _, tc := range []struct{ query, wantCode string }{
		{"?page=cero", "invalid_page"},
		{"?page=0", "invalid_page"},
		{"?per_page=muchas", "invalid_per_page"},
		{"?per_page=-1", "invalid_per_page"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			rec := call(mount(enums.OperatorRoleAdmin, store), http.MethodGet, "/contacts"+tc.query, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 - body %s", rec.Code, rec.Body)
			}
			if code, _ := errorOf(t, rec); code != tc.wantCode {
				t.Fatalf("the error code is %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// TestPerPageIsCappedSoTheCallerCannotChooseTheQuery protects the database from a page size nobody would page through.
func TestPerPageIsCappedSoTheCallerCannotChooseTheQuery(t *testing.T) {
	store, _ := seededContact()

	rec := call(mount(enums.OperatorRoleAdmin, store), http.MethodGet, "/contacts?per_page=100000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var page pageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the page is not json: %v", err)
	}
	if page.PerPage != maxPerPage {
		t.Fatalf("per_page came back as %d, want it capped at %d", page.PerPage, maxPerPage)
	}
}

// TestTheSegmentIsRefusedInsteadOfComingBackEmpty is why the filter validates: a page of zero contacts because the id was
// mistyped reads exactly like a label nobody has used, and the caller cannot tell the two apart (F33).
func TestTheSegmentIsRefusedInsteadOfComingBackEmpty(t *testing.T) {
	store, _ := seededContact()
	mine := labels.Label{ID: id.New(), AccountID: testAccount, Name: "Ventas", Color: "#ff8800"}
	engine := mount(enums.OperatorRoleAdmin, store, mine)

	for _, tc := range []struct {
		name   string
		query  string
		status int
		code   string
	}{
		{"un label_id que no es uuid", "?label_id=ventas", http.StatusBadRequest, "invalid_id"},
		{"una etiqueta de otra cuenta", "?label_id=" + id.New().String(), http.StatusNotFound, "label_not_found"},
		{"un operador logico inventado", "?label_id=" + mine.ID.String() + "&match=xor", http.StatusBadRequest, "invalid_match"},
		{"una de las buenas y una que no existe", "?label_id=" + mine.ID.String() + "&label_id=" + id.New().String(),
			http.StatusNotFound, "label_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(engine, http.MethodGet, "/contacts"+tc.query, "")
			if rec.Code != tc.status {
				t.Fatalf("got %d, want %d - body %s", rec.Code, tc.status, rec.Body)
			}
			if code, _ := errorOf(t, rec); code != tc.code {
				t.Fatalf("the code is %q, want %q", code, tc.code)
			}
		})
	}
}

// TestTheSameLabelAskedTwiceIsOneCriterion is what dedup buys: with match all, two copies of one label would ask for a count
// of two that no contact can reach, and the segment would be empty for no reason the caller could see.
func TestTheSameLabelAskedTwiceIsOneCriterion(t *testing.T) {
	store, contact := seededContact()
	ventas := labels.Label{ID: id.New(), AccountID: testAccount, Name: "Ventas", Color: "#ff8800"}
	store.tags[contact.ID] = []uuid.UUID{ventas.ID}

	engine := mount(enums.OperatorRoleAdmin, store, ventas)
	rec := call(engine, http.MethodGet, "/contacts?label_id="+ventas.ID.String()+"&label_id="+ventas.ID.String()+"&match=all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 - body %s", rec.Code, rec.Body)
	}

	var page struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("the listing is not json: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("the segment totals %d, want the contact that carries the label once", page.Total)
	}
}

// TestTheDefaultOperatorNarrows fixes which of the two a caller that named none gets, because the answer is a different book.
func TestTheDefaultOperatorNarrows(t *testing.T) {
	store, contact := seededContact()
	ventas := labels.Label{ID: id.New(), AccountID: testAccount, Name: "Ventas", Color: "#ff8800"}
	moroso := labels.Label{ID: id.New(), AccountID: testAccount, Name: "Moroso", Color: "#112233"}
	store.tags[contact.ID] = []uuid.UUID{ventas.ID}
	engine := mount(enums.OperatorRoleAdmin, store, ventas, moroso)

	both := "?label_id=" + ventas.ID.String() + "&label_id=" + moroso.ID.String()
	for _, tc := range []struct {
		query string
		total int
	}{
		{both, 0}, // nobody said which, and the default is the one that narrows
		{both + "&match=all", 0},
		{both + "&match=any", 1},
	} {
		rec := call(engine, http.MethodGet, "/contacts"+tc.query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d - body %s", tc.query, rec.Code, rec.Body)
		}
		var page struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("the listing is not json: %v", err)
		}
		if page.Total != tc.total {
			t.Fatalf("GET %s totals %d, want %d", tc.query, page.Total, tc.total)
		}
	}
}
