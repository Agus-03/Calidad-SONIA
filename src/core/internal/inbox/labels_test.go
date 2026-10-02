package inbox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// TestWhoLabelsIsWhoReadsTheThread is ADR-0057 as a table: the reach of the thread, resolved ones too, from the catalogue only.
func TestWhoLabelsIsWhoReadsTheThread(t *testing.T) {
	me, other, known := id.New(), id.New(), id.New()

	for _, tc := range []struct {
		name    string
		holder  *uuid.UUID
		role    enums.OperatorRole
		status  enums.ConversationStatus
		label   uuid.UUID
		wantErr error
	}{
		{"the vendor labels its own", &me, enums.OperatorRoleOperator, enums.ConversationStatusActive, known, nil},
		{"an operator labels one of the pool", nil, enums.OperatorRoleOperator, enums.ConversationStatusActive, known, nil},
		{"a supervisor labels a vendor's", &other, enums.OperatorRoleSupervisor, enums.ConversationStatusActive, known, nil},
		{"a resolved one is labelled too", &me, enums.OperatorRoleOperator, enums.ConversationStatusResolved, known, nil},
		{"another's does not exist for an operator", &other, enums.OperatorRoleOperator, enums.ConversationStatusActive, known, ErrNotFound},
		{"a label out of the catalogue", &me, enums.OperatorRoleOperator, enums.ConversationStatusActive, id.New(), ErrLabelNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(&stubStore{})
			d.catalogue.known[known] = true
			target := d.open(tc.holder)
			target.Status = tc.status
			d.store.targets[target.ConversationID] = target

			tags, err := d.service.AssignLabel(context.Background(), scopeOf(me, tc.role), target.ConversationID, tc.label)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("AssignLabel: got %v, want %v", err, tc.wantErr)
			}
			linked := len(d.store.labels[target.ConversationID]) == 1
			if linked != (tc.wantErr == nil) || (linked && (len(tags) != 1 || tags[0].ID != known)) {
				t.Fatalf("linked = %v and answered %v, want the label only when allowed", linked, tags)
			}
			if told := len(d.events.told) == 1; told != linked {
				t.Fatalf("told the hub %v, want only after a link", d.events.told)
			}
			// Labelling is not answering: it never takes the pool (ADR-0035 regla 13, ADR-0057).
			if len(d.aggregate.claimed) != 0 || len(d.store.assignments) != 0 {
				t.Fatal("labelling assigned the conversation")
			}
		})
	}
}

// TestLabellingAsksForTheEndState: twice is the same request twice, and removing what is not there is already done (ADR-0057).
func TestLabellingAsksForTheEndState(t *testing.T) {
	me, known := id.New(), id.New()
	d := newDesk(&stubStore{})
	d.catalogue.known[known] = true
	target := d.open(&me)
	scope := scopeOf(me, enums.OperatorRoleOperator)

	for range 2 {
		if _, err := d.service.AssignLabel(context.Background(), scope, target.ConversationID, known); err != nil {
			t.Fatalf("AssignLabel: %v", err)
		}
	}
	if len(d.store.labels[target.ConversationID]) != 1 || len(d.events.told) != 1 {
		t.Fatalf("two tags left %v links and told the hub %v, want one of each", d.store.labels[target.ConversationID], d.events.told)
	}
	for range 2 {
		tags, err := d.service.UnassignLabel(context.Background(), scope, target.ConversationID, known)
		if err != nil || len(tags) != 0 {
			t.Fatalf("UnassignLabel: got %v and %v, want no labels and no error", tags, err)
		}
	}
	if len(d.events.told) != 2 {
		t.Fatalf("told the hub %v, want once for the link and once for the removal", d.events.told)
	}
	// The catalogue is not asked to remove: a label of another account is simply not there.
	if _, err := d.service.UnassignLabel(context.Background(), scope, target.ConversationID, id.New()); err != nil {
		t.Fatalf("removing a label that was never there: got %v, want no error", err)
	}
}

// TestTheLabelRoutesAnswerWithTheirCodes walks both routes, because a wrong method or path compiles (ADR-0057).
func TestTheLabelRoutesAnswerWithTheirCodes(t *testing.T) {
	known := id.New()
	d := newDesk(&stubStore{})
	d.catalogue.known[known] = true
	target := d.open(&testOperator)
	base := "/conversations/" + target.ConversationID.String() + "/labels"
	engine := mountDesk(enums.OperatorRoleOperator, d)

	for _, tc := range []struct {
		method, path, body string
		want               int
		code               string
		contains           string
	}{
		{http.MethodPost, base, `{"label_id":"` + known.String() + `"}`, http.StatusOK, "", `"labels":[{"id":"` + known.String()},
		{http.MethodPost, base, `{}`, http.StatusBadRequest, "invalid_body", ""},
		{http.MethodPost, base, `{"label_id":"x"}`, http.StatusBadRequest, "invalid_label_id", ""},
		{http.MethodPost, base, `{"label_id":"` + id.New().String() + `"}`, http.StatusNotFound, "label_not_found", ""},
		{http.MethodPost, "/conversations/" + id.New().String() + "/labels", `{"label_id":"` + known.String() + `"}`, http.StatusNotFound, "conversation_not_found", ""},
		{http.MethodDelete, base + "/x", "", http.StatusBadRequest, "invalid_label_id", ""},
		{http.MethodDelete, base + "/" + known.String(), "", http.StatusOK, "", `"labels":[]`},
	} {
		rec := postJSON(engine, tc.method, tc.path, tc.body)
		if rec.Code != tc.want {
			t.Fatalf("%s %s: got %d, want %d - body %s", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
		if tc.code != "" {
			if got := errorCode(t, rec); got != tc.code {
				t.Fatalf("%s %s: code %q, want %q", tc.method, tc.path, got, tc.code)
			}
		}
		if !strings.Contains(rec.Body.String(), tc.contains) {
			t.Fatalf("%s %s: body %s, want it to hold %s", tc.method, tc.path, rec.Body, tc.contains)
		}
	}
}

// TestTheLabelsAreOnTheRow is F24 read through the listing, an empty list and never null for a row with none (ADR-0057).
func TestTheLabelsAreOnTheRow(t *testing.T) {
	label := id.New()
	store := &stubStore{rows: []Summary{
		{ID: id.New(), Labels: []Label{{ID: label, Name: "Ventas", Color: "#00ff00"}}, LastActivityAt: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{ID: id.New(), LastActivityAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
	}}

	rec := get(mountDesk(enums.OperatorRoleAdmin, newDesk(store)), url.Values{})
	body := rec.Body.String()
	if !strings.Contains(body, `"labels":[{"id":"`+label.String()+`","name":"Ventas","color":"#00ff00"}]`) || !strings.Contains(body, `"labels":[]`) {
		t.Fatalf("want one row with its label and one with an empty list - body %s", body)
	}
}
