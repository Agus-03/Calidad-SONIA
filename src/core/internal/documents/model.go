// Package documents owns knowledge_bases and documents: the admin's half of F06, which uploads, lists and removes the files an
// AIAgent answers from. The chunks are not here: the brain's worker writes them (regla 3, ADR-0064).
package documents

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
)

// MaxKnowledgeBaseBytes is the "límite 100 MB por agente" of F06, in binary megabytes: the sum of the live documents of one base.
const MaxKnowledgeBaseBytes int64 = 100 << 20

// The only format ADR-0064 admits (regla 10): the declared type and the signature both have to say PDF.
const pdfMIMEType = "application/pdf"

var pdfSignature = []byte("%PDF-")

// The name of a file in every common file system, so no real name is refused and no header can fill the column.
const maxFilenameRunes = 255

var (
	// ErrAgentNotFound covers an agent of another account and a removed one, so an id guessed across accounts reads as missing.
	ErrAgentNotFound = errors.New("documents: ai agent not found")

	// ErrDocumentNotFound covers a document of another agent too, and one already removed.
	ErrDocumentNotFound = errors.New("documents: document not found")

	// ErrNoticeNotAccepted is ADR-0066, regla 3: an upload that does not carry the version of the notice in force is refused.
	ErrNoticeNotAccepted = errors.New("documents: the personal data notice in force was not accepted")

	// ErrNotPDF is ADR-0064, regla 10: the declared type or the first bytes say it is not a PDF.
	ErrNotPDF = errors.New("documents: the file is not a pdf")

	// ErrInvalidFilename is a name that is not valid UTF-8 or is longer than the list can show, refused before the upload.
	ErrInvalidFilename = errors.New("documents: the file name is not valid")

	// ErrEmptyFile is refused before anything is encrypted: an empty file is not a PDF and would only waste a row.
	ErrEmptyFile = errors.New("documents: the file is empty")

	// ErrLimitExceeded is F06's 100 MB, checked before uploading and again under the lock of the base (ADR-0064, regla 6).
	ErrLimitExceeded = errors.New("documents: the knowledge base would pass its 100 MB")

	// ErrDuplicate is a live document of the same base with the same checksum: ingesting it twice repeats every chunk (ADR-0070).
	ErrDuplicate = errors.New("documents: the same file is already in the knowledge base")

	// errNoKnowledgeBase is an agent that never uploaded: removing one of its documents finds nothing to remove.
	errNoKnowledgeBase = errors.New("documents: the ai agent has no knowledge base")

	// ErrStorage wraps whatever the object store answered, so the controller tells the admin the store failed and not the core.
	ErrStorage = errors.New("documents: the object store did not take the file")
)

// Scope is who asks: the account bounds every read, and the operator is who uploaded (ADR-0066, regla 2).
type Scope struct {
	AccountID  uuid.UUID
	OperatorID uuid.UUID
}

// KnowledgeBase is the one of an AIAgent, created with its first upload (ADR-0064, regla 5).
type KnowledgeBase struct {
	ID             uuid.UUID
	AIAgentID      uuid.UUID
	CollectionName string
	SizeBytesTotal int64
}

type Document struct {
	ID                       uuid.UUID
	KnowledgeBaseID          uuid.UUID
	Filename                 string
	MIMEType                 string
	SizeBytes                int64
	Checksum                 string
	ObjectKey                string
	Status                   enums.DocumentStatus
	ErrorMessage             *string
	UploadedByOperatorID     uuid.UUID
	PersonalDataTermsVersion int16
	CreatedAt                time.Time
}

// Upload is what the controller read from the request, before anything is checked.
type Upload struct {
	Filename     string
	MIMEType     string
	Data         []byte
	TermsVersion int
}

// Listing carries the total so the admin sees how much of the 100 MB is left without adding the sizes up.
type Listing struct {
	Items          []Document
	SizeBytesTotal int64
}

// ObjectKey is derived from the two ids and nothing else, so the row and the object never disagree on it (ADR-0064, regla 5).
func ObjectKey(knowledgeBaseID, documentID uuid.UUID) string {
	return "knowledge-bases/" + knowledgeBaseID.String() + "/" + documentID.String()
}

// CollectionName is the Qdrant collection of the agent, written by the core when it creates the base (ADR-0010, ADR-0064 regla 5).
func CollectionName(agentID uuid.UUID) string {
	return "kb_" + agentID.String()
}
