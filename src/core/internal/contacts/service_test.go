package contacts

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/labels"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

func scopeOf(account, operator uuid.UUID, role enums.OperatorRole) Scope {
	return Scope{AccountID: account, OperatorID: operator, Role: role}
}

func newRow(account uuid.UUID, name, lastname string) Contact {
	return Contact{
		ID: id.New(), AccountID: account, PhoneWhatsapp: normalizePhone("54935112" + id.New().String()),
		Name: name, Lastname: lastname, Type: enums.ContactTypeClient, SourceChannel: enums.SourceChannelDirect,
	}
}

// TestCheckWritableIsTheTableOfTheDecision walks the rule field by field, because it is a table in ADR-0032 and a table is checked as one.
func TestCheckWritableIsTheTableOfTheDecision(t *testing.T) {
	everything := []string{FieldName, FieldLastname, FieldEmail, FieldType, FieldNotes, FieldOptOut, FieldBlocked}

	// The operator writes the two of regla 8 and is refused the other five, one by one, so the error names which.
	if err := CheckWritable(enums.OperatorRoleOperator, []string{FieldName, FieldLastname}); err != nil {
		t.Fatalf("the operator was refused its own two fields: %v", err)
	}
	for _, field := range []string{FieldEmail, FieldType, FieldNotes, FieldOptOut, FieldBlocked} {
		var refused ErrFieldNotWritable
		err := CheckWritable(enums.OperatorRoleOperator, []string{FieldName, field})
		if !errors.As(err, &refused) {
			t.Fatalf("the operator wrote %s and was allowed: %v", field, err)
		}
		if refused.Field != field {
			t.Fatalf("the refusal names %q, want %q", refused.Field, field)
		}
	}

	for _, role := range []enums.OperatorRole{enums.OperatorRoleSupervisor, enums.OperatorRoleAdmin} {
		if err := CheckWritable(role, everything); err != nil {
			t.Fatalf("%s was refused a field of regla 9: %v", role, err)
		}
	}

	// A role nobody declared writes nothing, which is the safe side of the default.
	if err := CheckWritable("", []string{FieldName}); err == nil {
		t.Fatalf("an undeclared role was allowed to write, want a refusal")
	}
}

// TestApplyOnlyMovesWhatTheEditTouched is what makes a partial update partial: the fields nobody sent keep the value the row had.
func TestApplyOnlyMovesWhatTheEditTouched(t *testing.T) {
	ctx := context.Background()
	account, operator := id.New(), id.New()

	row := newRow(account, "Ana", "Perez")
	row.Email, row.Notes, row.Type = "ana@coop.test", "prefiere la tarde", enums.ContactTypeProspect
	store := newStubStore(row)
	service := NewService(store, newStubCatalogue(), nil)

	got, err := service.Apply(ctx, scopeOf(account, operator, enums.OperatorRoleAdmin), row.ID, func(ch *Changes) {
		ch.Name = "Ana Maria"
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got.Name != "Ana Maria" {
		t.Fatalf("the name is %q, want it corrected", got.Name)
	}
	if got.Email != "ana@coop.test" || got.Notes != "prefiere la tarde" || got.Type != enums.ContactTypeProspect {
		t.Fatalf("an untouched field moved: %+v", got)
	}
}

// TestApplyRefusesToLeaveAContactNameless guards the two NOT NULL columns against a value that satisfies them and means nothing.
func TestApplyRefusesToLeaveAContactNameless(t *testing.T) {
	ctx := context.Background()
	account, operator := id.New(), id.New()

	row := newRow(account, "Bruno", "Diaz")
	store := newStubStore(row)
	service := NewService(store, newStubCatalogue(), nil)
	scope := scopeOf(account, operator, enums.OperatorRoleAdmin)

	for _, blank := range []string{"", "   "} {
		if _, err := service.Apply(ctx, scope, row.ID, func(ch *Changes) { ch.Name = blank }); !errors.Is(err, ErrEmptyName) {
			t.Fatalf("Apply with a blank name: got %v, want ErrEmptyName", err)
		}
	}
	if store.rows[0].Name != "Bruno" {
		t.Fatalf("the row now reads %q, want it untouched", store.rows[0].Name)
	}
}

// TestApplyRefusesATypeOutsideTheContract keeps a bad value from reaching the CHECK and coming back as a 500 (C07).
func TestApplyRefusesATypeOutsideTheContract(t *testing.T) {
	ctx := context.Background()
	account := id.New()

	row := newRow(account, "Carla", "Gomez")
	service := NewService(newStubStore(row), newStubCatalogue(), nil)

	_, err := service.Apply(ctx, scopeOf(account, id.New(), enums.OperatorRoleAdmin), row.ID, func(ch *Changes) {
		ch.Type = "vip"
	})
	if err == nil {
		t.Fatalf("Apply accepted a type outside the contract, want a refusal")
	}
}

// TestEveryWriteReadsThroughTheScopeFirst is what keeps the rule from being a read-only rule: a contact out of reach cannot be
// edited, cannot have its four fields written and cannot be taken from the whole team either.
func TestEveryWriteReadsThroughTheScopeFirst(t *testing.T) {
	ctx := context.Background()
	account, mine, theirs := id.New(), id.New(), id.New()

	row := newRow(account, "Diego", "Luna")
	store := newStubStore(row)
	store.assign(theirs, row.ID)
	service := NewService(store, newStubCatalogue(), nil)

	// An operator with no conversation on this contact: every write must answer as if the contact were not there.
	scope := scopeOf(account, mine, enums.OperatorRoleOperator)

	if _, err := service.Apply(ctx, scope, row.ID, func(ch *Changes) { ch.Name = "Robado" }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Apply out of reach: got %v, want ErrNotFound", err)
	}
	if _, err := service.UpdateSensitive(ctx, scope, row.ID, Sensitive{DNI: "36415782"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateSensitive out of reach: got %v, want ErrNotFound", err)
	}
	if err := service.Delete(ctx, scope, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete out of reach: got %v, want ErrNotFound", err)
	}
	if store.rows[0].Name != "Diego" {
		t.Fatalf("the row was touched from out of reach: %q", store.rows[0].Name)
	}
}

// TestCreateStampsWhatTheCallerDoesNotChoose covers the three the request never carries: the id, the account and the consent.
func TestCreateStampsWhatTheCallerDoesNotChoose(t *testing.T) {
	ctx := context.Background()
	account, operator := id.New(), id.New()
	service := NewService(newStubStore(), newStubCatalogue(), nil)

	created, err := service.Create(ctx, scopeOf(account, operator, enums.OperatorRoleAdmin), New{
		PhoneWhatsapp: "+54 9 351 123 4567", Name: "Elsa", Lastname: "Mora",
		Type: enums.ContactTypeClient, SourceChannel: enums.SourceChannelQr,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil || created.AccountID != account {
		t.Fatalf("the contact is %+v, want an id and the account of the actor", created)
	}
	if created.ConsentGivenAt != nil {
		t.Fatalf("the consent was stamped at %v, want nil because nobody asked the client", created.ConsentGivenAt)
	}
}

// TestCreateRefusesAPhoneThatIsNoDigits is the case normalization creates: a number typed as punctuation arrives here as nothing.
func TestCreateRefusesAPhoneThatIsNoDigits(t *testing.T) {
	ctx := context.Background()
	account := id.New()
	service := NewService(newStubStore(), newStubCatalogue(), nil)

	for _, phone := range []string{"", "   ", "+-()", "sin numero"} {
		_, err := service.Create(ctx, scopeOf(account, id.New(), enums.OperatorRoleAdmin), New{
			PhoneWhatsapp: phone, Name: "Fabio", Lastname: "Nieva",
			Type: enums.ContactTypeClient, SourceChannel: enums.SourceChannelDirect,
		})
		if !errors.Is(err, ErrEmptyPhone) {
			t.Fatalf("Create(%q): got %v, want ErrEmptyPhone", phone, err)
		}
	}
}

// TestALabelOfAnotherAccountIsRefused is the check the foreign key cannot do: contact_labels.label_id proves the label exists,
// never that it belongs to the account of the contact being tagged (ADR-0032, regla 13).
func TestALabelOfAnotherAccountIsRefused(t *testing.T) {
	ctx := context.Background()
	account, stranger := id.New(), id.New()

	row := newRow(account, "Gaston", "Ruiz")
	store := newStubStore(row)
	theirs := labels.Label{ID: id.New(), AccountID: stranger, Name: "Ventas", Color: "#ff8800"}
	service := NewService(store, newStubCatalogue(theirs), nil)

	err := service.AssignLabel(ctx, scopeOf(account, id.New(), enums.OperatorRoleAdmin), row.ID, theirs.ID)
	if !errors.Is(err, ErrLabelNotInAccount) {
		t.Fatalf("AssignLabel with a label of another account: got %v, want ErrLabelNotInAccount", err)
	}
	if len(store.tags[row.ID]) != 0 {
		t.Fatalf("the contact was tagged anyway: %v", store.tags[row.ID])
	}
}

// TestTaggingTwiceLeavesOneTag is what the unique constraint buys: the request says what the contact should carry, so asking
// for it again is not a conflict.
func TestTaggingTwiceLeavesOneTag(t *testing.T) {
	ctx := context.Background()
	account := id.New()

	row := newRow(account, "Hugo", "Sosa")
	store := newStubStore(row)
	tag := labels.Label{ID: id.New(), AccountID: account, Name: "Ventas", Color: "#ff8800"}
	service := NewService(store, newStubCatalogue(tag), nil)
	scope := scopeOf(account, id.New(), enums.OperatorRoleAdmin)

	for range 2 {
		if err := service.AssignLabel(ctx, scope, row.ID, tag.ID); err != nil {
			t.Fatalf("AssignLabel: %v", err)
		}
	}

	found, err := service.LabelsOf(ctx, scope, row.ID)
	if err != nil {
		t.Fatalf("LabelsOf: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("the contact carries %d tags, want exactly one", len(found))
	}

	// And removing one that was never there is not an error either, for the same reason: the end state is what was asked for.
	if err := service.UnassignLabel(ctx, scope, row.ID, id.New()); err != nil {
		t.Fatalf("UnassignLabel of a tag that was never there: %v", err)
	}
	if err := service.UnassignLabel(ctx, scope, row.ID, tag.ID); err != nil {
		t.Fatalf("UnassignLabel: %v", err)
	}
	if found, _ := service.LabelsOf(ctx, scope, row.ID); len(found) != 0 {
		t.Fatalf("the tag survived the removal: %v", found)
	}
}

// TestTaggingReadsThroughTheScopeToo keeps the three label methods inside the rule the rest of the writes obey: a contact an
// operator does not reach cannot be tagged, untagged, or have its tags read.
func TestTaggingReadsThroughTheScopeToo(t *testing.T) {
	ctx := context.Background()
	account, mine, theirs := id.New(), id.New(), id.New()

	row := newRow(account, "Irene", "Paz")
	store := newStubStore(row)
	store.assign(theirs, row.ID)
	tag := labels.Label{ID: id.New(), AccountID: account, Name: "Ventas", Color: "#ff8800"}
	service := NewService(store, newStubCatalogue(tag), nil)

	scope := scopeOf(account, mine, enums.OperatorRoleOperator)

	if err := service.AssignLabel(ctx, scope, row.ID, tag.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AssignLabel out of reach: got %v, want ErrNotFound", err)
	}
	if err := service.UnassignLabel(ctx, scope, row.ID, tag.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UnassignLabel out of reach: got %v, want ErrNotFound", err)
	}
	if _, err := service.LabelsOf(ctx, scope, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LabelsOf out of reach: got %v, want ErrNotFound", err)
	}
	if len(store.tags[row.ID]) != 0 {
		t.Fatalf("the contact was tagged from out of reach: %v", store.tags[row.ID])
	}
}
