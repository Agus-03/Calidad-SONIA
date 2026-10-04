package documents

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

type fixture struct {
	scope   Scope
	agentID uuid.UUID
	store   *stubStore
	objects *stubObjects
	service *Service
}

func newFixture() fixture {
	scope := Scope{AccountID: id.New(), OperatorID: id.New()}
	agentID := id.New()
	store := newStubStore(scope.AccountID, agentID)
	objects := newStubObjects()
	return fixture{
		scope: scope, agentID: agentID, store: store, objects: objects,
		service: NewService(Options{Repo: store, Tx: store.tx, Objects: objects, Sealer: stubSealer{}}),
	}
}

// TestAnUploadLeavesTheSealedObjectAndAPendingRow is ADR-0064, regla 6, end to end: the base, the object, the row and the size.
func TestAnUploadLeavesTheSealedObjectAndAPendingRow(t *testing.T) {
	f := newFixture()
	up := validUpload("lista de precios")
	up.Filename = `C:\fakepath\precios.pdf`

	doc, err := f.service.Upload(t.Context(), f.scope, f.agentID, up)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	kb := f.store.bases[f.agentID]
	if kb.CollectionName != "kb_"+f.agentID.String() {
		t.Fatalf("collection = %q, want kb_<ai_agent_id> (ADR-0064, regla 5)", kb.CollectionName)
	}
	if doc.Status != enums.DocumentStatusPending || doc.KnowledgeBaseID != kb.ID || doc.UploadedByOperatorID != f.scope.OperatorID {
		t.Fatalf("document = %+v, want pending, in the agent's base, by the operator who asked", doc)
	}
	if doc.Filename != "precios.pdf" || doc.MIMEType != "application/pdf" || doc.PersonalDataTermsVersion != NoticeVersion {
		t.Fatalf("document = %+v, want the bare filename, the pdf type and the notice version", doc)
	}
	sum := sha256.Sum256(up.Data)
	if doc.Checksum != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum = %s, want the SHA-256 of the PDF in the clear (ADR-0064, regla 4)", doc.Checksum)
	}
	if doc.ObjectKey != "knowledge-bases/"+kb.ID.String()+"/"+doc.ID.String() {
		t.Fatalf("object key = %q, want knowledge-bases/<kb>/<document>", doc.ObjectKey)
	}
	stored := f.objects.objects[doc.ObjectKey]
	if !bytes.Equal(stored, append([]byte("sealed:"), up.Data...)) {
		t.Fatalf("the store holds %q, want the sealed file and never the PDF", stored)
	}
	if kb.SizeBytesTotal != int64(len(up.Data)) {
		t.Fatalf("size of the base = %d, want the %d bytes of the file", kb.SizeBytesTotal, len(up.Data))
	}
}

// TestWhatIsNotAnAcceptedPDFNeverReachesTheStore: every refusal happens before a byte leaves (ADR-0064 regla 10, ADR-0066 regla 3).
func TestWhatIsNotAnAcceptedPDFNeverReachesTheStore(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Upload)
		want   error
	}{
		{"sin aceptar el aviso", func(u *Upload) { u.TermsVersion = 0 }, ErrNoticeNotAccepted},
		{"con un aviso viejo", func(u *Upload) { u.TermsVersion = NoticeVersion + 1 }, ErrNoticeNotAccepted},
		{"vacio", func(u *Upload) { u.Data = nil }, ErrEmptyFile},
		{"declarado como otra cosa", func(u *Upload) { u.MIMEType = "image/png" }, ErrNotPDF},
		{"declarado pdf sin la firma", func(u *Upload) { u.Data = []byte("hola, no soy un pdf") }, ErrNotPDF},
		{"mas grande que la base entera", func(u *Upload) { u.Data = append(pdf(""), make([]byte, MaxKnowledgeBaseBytes)...) }, ErrLimitExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			up := validUpload("x")
			tc.mutate(&up)
			if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, up); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if len(f.objects.objects) != 0 {
				t.Fatalf("a refused upload left %d objects in the store", len(f.objects.objects))
			}
		})
	}

	// A type with parameters is still a PDF: browsers are free to add them.
	f := newFixture()
	up := validUpload("x")
	up.MIMEType = "Application/PDF; charset=binary"
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, up); err != nil {
		t.Fatalf("a pdf with parameters in its type: %v", err)
	}
}

// TestAnAgentOfAnotherAccountReadsAsMissing: the three operations ask for the agent before anything else (ADR-0012).
func TestAnAgentOfAnotherAccountReadsAsMissing(t *testing.T) {
	f := newFixture()
	other := Scope{AccountID: id.New(), OperatorID: id.New()}

	if _, err := f.service.Upload(t.Context(), other, f.agentID, validUpload("x")); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Upload: got %v, want ErrAgentNotFound", err)
	}
	if _, err := f.service.List(t.Context(), other, f.agentID); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("List: got %v, want ErrAgentNotFound", err)
	}
	if err := f.service.Delete(t.Context(), other, f.agentID, id.New()); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("Delete: got %v, want ErrAgentNotFound", err)
	}
}

// TestTheSameFileTwiceIsRefused is ADR-0070: a duplicate would repeat every chunk in the search. Removing it frees the checksum.
func TestTheSameFileTwiceIsRefused(t *testing.T) {
	f := newFixture()
	first, err := f.service.Upload(t.Context(), f.scope, f.agentID, validUpload("contrato"))
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	renamed := validUpload("contrato")
	renamed.Filename = "otro-nombre.pdf"
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, renamed); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("the same bytes under another name: got %v, want ErrDuplicate", err)
	}
	if len(f.objects.objects) != 1 {
		t.Fatalf("the duplicate reached the store: %d objects", len(f.objects.objects))
	}

	if err := f.service.Delete(t.Context(), f.scope, f.agentID, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, renamed); err != nil {
		t.Fatalf("the same file after removing it: %v", err)
	}
}

// TestTheLimitIsCheckedAgainUnderTheLock: two uploads that each fit alone cannot both enter (ADR-0064, regla 6 (c)), and the one
// that loses leaves no object behind.
func TestTheLimitIsCheckedAgainUnderTheLock(t *testing.T) {
	f := newFixture()
	half := MaxKnowledgeBaseBytes/2 + 1
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, Upload{
		Filename: "a.pdf", MIMEType: "application/pdf", Data: append(pdf("a"), make([]byte, half)...), TermsVersion: NoticeVersion,
	}); err != nil {
		t.Fatalf("the first half: %v", err)
	}
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, Upload{
		Filename: "b.pdf", MIMEType: "application/pdf", Data: append(pdf("b"), make([]byte, half)...), TermsVersion: NoticeVersion,
	}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("past the 100 MB before uploading: got %v, want ErrLimitExceeded", err)
	}

	// The race: the base had room when the upload checked, and someone filled it before the lock.
	g := newFixture()
	g.store.beforeLock = func(s *stubStore) {
		kb := s.bases[g.agentID]
		kb.SizeBytesTotal = MaxKnowledgeBaseBytes
		s.bases[g.agentID] = kb
	}
	if _, err := g.service.Upload(t.Context(), g.scope, g.agentID, validUpload("tarde")); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("the base filled under the lock: got %v, want ErrLimitExceeded", err)
	}
	if len(g.objects.objects) != 0 || len(g.objects.deleted) != 1 {
		t.Fatalf("the losing upload left %d objects and deleted %d, want its object gone", len(g.objects.objects), len(g.objects.deleted))
	}

	// The same race with the same file: the other upload was this one.
	h := newFixture()
	h.store.beforeLock = func(s *stubStore) {
		up := validUpload("doble")
		sum := sha256.Sum256(up.Data)
		s.docs[id.New()] = Document{KnowledgeBaseID: s.bases[h.agentID].ID, Checksum: hex.EncodeToString(sum[:])}
	}
	if _, err := h.service.Upload(t.Context(), h.scope, h.agentID, validUpload("doble")); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("the same file entered under the lock: got %v, want ErrDuplicate", err)
	}
}

// TestARowThatCannotBeWrittenTakesItsObjectWithIt is ADR-0064, regla 6 (d): one attempt, and the key to the log if that fails.
func TestARowThatCannotBeWrittenTakesItsObjectWithIt(t *testing.T) {
	f := newFixture()
	f.store.insertErr = errBoom
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, validUpload("x")); !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want the insert's error", err)
	}
	if len(f.objects.objects) != 0 || len(f.objects.deleted) != 1 {
		t.Fatalf("after a failed insert: %d objects, %d deletes, want the object deleted once", len(f.objects.objects), len(f.objects.deleted))
	}
	if f.store.bases[f.agentID].SizeBytesTotal != 0 {
		t.Fatalf("the size moved for a row that was never written")
	}

	g := newFixture()
	g.store.insertErr = errBoom
	g.objects.deleteErr = errBoom
	if _, err := g.service.Upload(t.Context(), g.scope, g.agentID, validUpload("x")); !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want the insert's error even when the cleanup fails too", err)
	}
	if len(g.objects.deleted) != 1 {
		t.Fatalf("the cleanup was tried %d times, want once: nothing sweeps orphans", len(g.objects.deleted))
	}
}

// TestAStoreOrACipherThatFailsLeavesNoRow: without the object there is nothing for the worker to read, so no row is written.
func TestAStoreOrACipherThatFailsLeavesNoRow(t *testing.T) {
	f := newFixture()
	f.objects.putErr = errBoom
	if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, validUpload("x")); !errors.Is(err, ErrStorage) {
		t.Fatalf("got %v, want ErrStorage", err)
	}
	if len(f.store.docs) != 0 {
		t.Fatalf("a row was written for a file the store did not take")
	}

	g := newFixture()
	g.service = NewService(Options{Repo: g.store, Tx: g.store.tx, Objects: g.objects, Sealer: stubSealer{err: errBoom}})
	if _, err := g.service.Upload(t.Context(), g.scope, g.agentID, validUpload("x")); !errors.Is(err, errBoom) {
		t.Fatalf("got %v, want the cipher's error", err)
	}
	if len(g.objects.objects) != 0 {
		t.Fatalf("a file the cipher could not seal reached the store")
	}

	h := newFixture()
	h.store.readErr = errBoom
	if _, err := h.service.Upload(t.Context(), h.scope, h.agentID, validUpload("x")); !errors.Is(err, errBoom) {
		t.Fatalf("Upload with a failing read: got %v", err)
	}
	if _, err := h.service.List(t.Context(), h.scope, h.agentID); !errors.Is(err, errBoom) {
		t.Fatalf("List with a failing read: got %v", err)
	}
}

// TestTheListingAndTheRemovalMoveTheTotal is ADR-0064, regla 9: the row is marked and its size given back; the object stays.
func TestTheListingAndTheRemovalMoveTheTotal(t *testing.T) {
	f := newFixture()

	empty, err := f.service.List(t.Context(), f.scope, f.agentID)
	if err != nil || len(empty.Items) != 0 || empty.SizeBytesTotal != 0 {
		t.Fatalf("an agent that never uploaded: got %+v and %v, want nothing and zero bytes", empty, err)
	}

	first, _ := f.service.Upload(t.Context(), f.scope, f.agentID, validUpload("uno"))
	second, _ := f.service.Upload(t.Context(), f.scope, f.agentID, validUpload("dos"))
	listing, err := f.service.List(t.Context(), f.scope, f.agentID)
	if err != nil || len(listing.Items) != 2 || listing.Items[0].ID != second.ID {
		t.Fatalf("List: got %+v and %v, want the two, newest first", listing, err)
	}
	if listing.SizeBytesTotal != first.SizeBytes+second.SizeBytes {
		t.Fatalf("total = %d, want %d", listing.SizeBytesTotal, first.SizeBytes+second.SizeBytes)
	}

	if err := f.service.Delete(t.Context(), f.scope, f.agentID, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	after, _ := f.service.List(t.Context(), f.scope, f.agentID)
	if len(after.Items) != 1 || after.SizeBytesTotal != second.SizeBytes {
		t.Fatalf("after the removal: %+v, want one document and its size", after)
	}
	if _, kept := f.objects.objects[first.ObjectKey]; !kept {
		t.Fatalf("the removal deleted the object, and ADR-0064 regla 9 keeps it")
	}

	if err := f.service.Delete(t.Context(), f.scope, f.agentID, first.ID); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("removing twice: got %v, want ErrDocumentNotFound", err)
	}

	// An agent that never uploaded has no base to lock, and nothing to remove.
	g := newFixture()
	if err := g.service.Delete(t.Context(), g.scope, g.agentID, id.New()); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("removing from an agent without a base: got %v, want ErrDocumentNotFound", err)
	}
}

func TestCleanFilenameKeepsWhatTheAdminSees(t *testing.T) {
	for in, want := range map[string]string{
		"precios.pdf":                  "precios.pdf",
		"  contrato.pdf ":              "contrato.pdf",
		"carpeta/sub/planos.pdf":       "planos.pdf",
		`C:\Users\admin\politicas.pdf`: "politicas.pdf",
		"":                             "documento.pdf",
		"carpeta/":                     "documento.pdf",
		"precios\x00.pdf":              "precios.pdf",
		"señas\t y depósito.pdf":       "señas y depósito.pdf",
	} {
		if got, err := cleanFilename(in); err != nil || got != want {
			t.Fatalf("cleanFilename(%q) = %q and %v, want %q", in, got, err, want)
		}
	}
}

// TestANameTheColumnWouldRefuseIsRefusedBeforeTheUpload: invalid UTF-8 or a name past 255 characters is a 400, and not a 500
// after up to 100 MB went to the store and back.
func TestANameTheColumnWouldRefuseIsRefusedBeforeTheUpload(t *testing.T) {
	for _, name := range []string{"precios\xff.pdf", strings.Repeat("a", maxFilenameRunes+1)} {
		f := newFixture()
		up := validUpload("x")
		up.Filename = name
		if _, err := f.service.Upload(t.Context(), f.scope, f.agentID, up); !errors.Is(err, ErrInvalidFilename) {
			t.Fatalf("Upload of %q: got %v, want ErrInvalidFilename", name, err)
		}
		if len(f.objects.objects) != 0 {
			t.Fatalf("a refused name reached the store")
		}
	}
	if got, err := cleanFilename(strings.Repeat("ñ", maxFilenameRunes)); err != nil || got == "" {
		t.Fatalf("255 characters of two bytes each: got %v, want it accepted, because the limit counts characters", err)
	}
}
