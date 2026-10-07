package documents

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	soniahttp "github.com/mateo-velasquez/sonia/src/core/internal/platform/http"
)

// The multipart envelope around the file: the boundaries, the headers of each part and the version field.
const multipartOverhead = 1 << 20

type Controller struct {
	service *Service
	log     *slog.Logger
}

func NewController(service *Service, log *slog.Logger) *Controller {
	if log == nil {
		log = slog.Default()
	}
	return &Controller{service: service, log: log}
}

type noticeResponse struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// No object key and no checksum: they are the core's and the worker's, and the admin has nothing to do with either.
type documentResponse struct {
	ID                       string  `json:"id"`
	Filename                 string  `json:"filename"`
	MIMEType                 string  `json:"mime_type"`
	SizeBytes                int64   `json:"size_bytes"`
	Status                   string  `json:"status"`
	ErrorMessage             *string `json:"error_message"`
	UploadedByOperatorID     string  `json:"uploaded_by_operator_id"`
	PersonalDataTermsVersion int16   `json:"personal_data_terms_version"`
	CreatedAt                string  `json:"created_at"`
}

type listResponse struct {
	Items          []documentResponse `json:"items"`
	SizeBytesTotal int64              `json:"size_bytes_total"`
	SizeBytesLimit int64              `json:"size_bytes_limit"`
}

func present(d Document) documentResponse {
	return documentResponse{
		ID: d.ID.String(), Filename: d.Filename, MIMEType: d.MIMEType, SizeBytes: d.SizeBytes, Status: string(d.Status),
		ErrorMessage: d.ErrorMessage, UploadedByOperatorID: d.UploadedByOperatorID.String(),
		PersonalDataTermsVersion: d.PersonalDataTermsVersion, CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// notice is what the admin reads and accepts before uploading; the upload sends back the version it got (ADR-0066, regla 3).
func (c *Controller) notice(gc *gin.Context) {
	soniahttp.OK(gc, noticeResponse{Version: NoticeVersion, Text: NoticeText})
}

func (c *Controller) list(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	agentID, ok := c.uuidParam(gc, "id", "invalid_ai_agent_id", "the ai agent id is not a uuid")
	if !ok {
		return
	}
	found, err := c.service.List(gc.Request.Context(), scope, agentID)
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	items := make([]documentResponse, 0, len(found.Items))
	for _, d := range found.Items {
		items = append(items, present(d))
	}
	soniahttp.OK(gc, listResponse{Items: items, SizeBytesTotal: found.SizeBytesTotal, SizeBytesLimit: MaxKnowledgeBaseBytes})
}

// upload reads a multipart body with two fields: file, the PDF, and personal_data_terms_version, the notice accepted.
func (c *Controller) upload(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	agentID, ok := c.uuidParam(gc, "id", "invalid_ai_agent_id", "the ai agent id is not a uuid")
	if !ok {
		return
	}

	// Bounded before anything parses it: a body past one whole base cannot hold a document that fits in one.
	gc.Request.Body = http.MaxBytesReader(gc.Writer, gc.Request.Body, MaxKnowledgeBaseBytes+multipartOverhead)
	header, err := gc.FormFile("file")
	if err != nil {
		soniahttp.Fail(gc, c.log, readFailure(err))
		return
	}
	file, err := header.Open()
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest("invalid_file", "the file could not be read").WithCause(err))
		return
	}
	defer func() { _ = file.Close() }()
	// Allocated once at the size the multipart reader measured, and not grown by doubling as io.ReadAll does: the peak stays the
	// file and its ciphertext that ADR-0070, regla 2, declares. One byte past the limit, so a file that does not fit is told apart
	// from one that fills it exactly.
	data := make([]byte, min(header.Size, MaxKnowledgeBaseBytes+1))
	if _, err := io.ReadFull(file, data); err != nil {
		soniahttp.Fail(gc, c.log, readFailure(err))
		return
	}

	// A missing or unreadable version is a notice nobody accepted, and the service refuses it as such (ADR-0066, regla 3).
	terms, _ := strconv.Atoi(strings.TrimSpace(gc.PostForm("personal_data_terms_version")))

	doc, err := c.service.Upload(gc.Request.Context(), scope, agentID, Upload{
		Filename: header.Filename, MIMEType: header.Header.Get("Content-Type"), Data: data, TermsVersion: terms,
	})
	if err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.Created(gc, present(doc))
}

func (c *Controller) remove(gc *gin.Context) {
	scope, ok := c.scopeFrom(gc)
	if !ok {
		return
	}
	agentID, ok := c.uuidParam(gc, "id", "invalid_ai_agent_id", "the ai agent id is not a uuid")
	if !ok {
		return
	}
	documentID, ok := c.uuidParam(gc, "documentId", "invalid_document_id", "the document id is not a uuid")
	if !ok {
		return
	}
	if err := c.service.Delete(gc.Request.Context(), scope, agentID, documentID); err != nil {
		soniahttp.Fail(gc, c.log, translate(err))
		return
	}
	soniahttp.NoContent(gc)
}

// translate turns what the package knows into what the frontend switches on, and lets anything else fall through as a 500.
func translate(err error) error {
	switch {
	case errors.Is(err, ErrAgentNotFound):
		return soniahttp.NotFound("ai_agent_not_found", "no such ai agent").WithCause(err)
	case errors.Is(err, ErrDocumentNotFound):
		return soniahttp.NotFound("document_not_found", "no such document").WithCause(err)
	case errors.Is(err, ErrNoticeNotAccepted):
		return soniahttp.BadRequest("notice_not_accepted", "the personal data notice in force has to be accepted to upload").
			WithDetails(map[string]string{"personal_data_terms_version": strconv.Itoa(NoticeVersion)}).WithCause(err)
	case errors.Is(err, ErrInvalidFilename):
		return soniahttp.BadRequest("invalid_filename", "the file name has to be valid text of up to 255 characters").WithCause(err)
	case errors.Is(err, ErrEmptyFile):
		return soniahttp.BadRequest("empty_file", "the file is empty").WithCause(err)
	case errors.Is(err, ErrNotPDF):
		return soniahttp.BadRequest("not_a_pdf", "only pdf files are accepted").WithCause(err)
	case errors.Is(err, ErrLimitExceeded):
		return soniahttp.TooLarge("knowledge_base_full", "the knowledge base of an ai agent holds up to 100 MB").WithCause(err)
	case errors.Is(err, ErrDuplicate):
		return soniahttp.Conflict("document_duplicate", "the same file is already in this knowledge base").WithCause(err)
	case errors.Is(err, ErrStorage):
		return soniahttp.Unavailable("storage_unavailable", "the file could not be stored, try again later").WithCause(err)
	default:
		return err
	}
}

// readFailure tells a body past the limit from a request without a file, which are a 413 and a 400.
func readFailure(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return soniahttp.TooLarge("knowledge_base_full", "the knowledge base of an ai agent holds up to 100 MB").WithCause(err)
	}
	return soniahttp.BadRequest("missing_file", "a multipart body with a file field is required").WithCause(err)
}

func (c *Controller) uuidParam(gc *gin.Context, name, code, message string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(gc.Param(name))
	if err != nil {
		soniahttp.Fail(gc, c.log, soniahttp.BadRequest(code, message).WithCause(err))
		return uuid.Nil, false
	}
	return parsed, true
}

func (c *Controller) scopeFrom(gc *gin.Context) (Scope, bool) {
	actor, ok := soniahttp.ActorFrom(gc)
	if !ok {
		soniahttp.Fail(gc, c.log, soniahttp.Unauthorized("missing_credentials", "no operator identity in the request"))
		return Scope{}, false
	}
	return Scope{AccountID: actor.AccountID, OperatorID: actor.OperatorID}, true
}
