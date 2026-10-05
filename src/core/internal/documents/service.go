package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mateo-velasquez/sonia/src/core/internal/platform/enums"
	"github.com/mateo-velasquez/sonia/src/core/internal/platform/id"
)

// Store is the slice of the repository the service uses, declared here so the caller side owns the interface.
type Store interface {
	AgentExists(ctx context.Context, accountID, agentID uuid.UUID) (bool, error)
	EnsureKnowledgeBase(ctx context.Context, kb KnowledgeBase) (KnowledgeBase, error)
	KnowledgeBase(ctx context.Context, agentID uuid.UUID) (KnowledgeBase, bool, error)
	KnowledgeBaseForUpdate(ctx context.Context, agentID uuid.UUID) (KnowledgeBase, error)
	AddSize(ctx context.Context, knowledgeBaseID uuid.UUID, delta int64) error
	ChecksumTaken(ctx context.Context, knowledgeBaseID uuid.UUID, checksum string) (bool, error)
	InsertDocument(ctx context.Context, d Document) (Document, error)
	List(ctx context.Context, accountID, agentID uuid.UUID) ([]Document, error)
	DocumentForUpdate(ctx context.Context, accountID, agentID, documentID uuid.UUID) (Document, error)
	SoftDeleteDocument(ctx context.Context, documentID uuid.UUID) error
}

// Objects is the port of the object store (ADR-0017, ADR-0019): platform/storage's Firebase satisfies it, and so does a test.
type Objects interface {
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

// Sealer is the document key's cipher, and not the one of the sensitive columns (ADR-0064, regla 2).
type Sealer interface {
	EncryptBytes(plaintext []byte) ([]byte, error)
}

type Options struct {
	Repo Store
	// Binds the repository to one transaction: the row, the size of the base and the lock commit together (regla 5).
	Tx      func(ctx context.Context, fn func(Store) error) error
	Objects Objects
	Sealer  Sealer
	Logger  *slog.Logger
}

type Service struct {
	repo    Store
	tx      func(ctx context.Context, fn func(Store) error) error
	objects Objects
	sealer  Sealer
	log     *slog.Logger
}

func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{repo: opts.Repo, tx: opts.Tx, objects: opts.Objects, sealer: opts.Sealer, log: opts.Logger}
}

// Upload is ADR-0064, regla 6, in its order: check, then the object, then the row; and the object goes if the row cannot.
func (s *Service) Upload(ctx context.Context, scope Scope, agentID uuid.UUID, up Upload) (Document, error) {
	if up.TermsVersion != NoticeVersion {
		return Document{}, ErrNoticeNotAccepted
	}
	if len(up.Data) == 0 {
		return Document{}, ErrEmptyFile
	}
	if !isPDF(up) {
		return Document{}, ErrNotPDF
	}
	size := int64(len(up.Data))
	if size > MaxKnowledgeBaseBytes {
		return Document{}, ErrLimitExceeded
	}
	filename, err := cleanFilename(up.Filename)
	if err != nil {
		return Document{}, err
	}
	if err := s.agentExists(ctx, scope, agentID); err != nil {
		return Document{}, err
	}

	// The base is created before the object, because the object's key carries its id (ADR-0064, regla 5). An upload that fails
	// after this leaves an empty base, which is what the agent's next upload would have created anyway.
	kb, err := s.repo.EnsureKnowledgeBase(ctx, KnowledgeBase{ID: id.New(), AIAgentID: agentID, CollectionName: CollectionName(agentID)})
	if err != nil {
		return Document{}, err
	}

	// The hash of the PDF in the clear, before encrypting: with a random nonce, the hash of the ciphertext changes on every upload
	// of the same file and could never find a duplicate (ADR-0064, regla 4).
	sum := sha256.Sum256(up.Data)
	checksum := hex.EncodeToString(sum[:])

	// Checked before the upload too, so a file that cannot fit or is already there costs no transfer. The transaction checks again.
	if kb.SizeBytesTotal+size > MaxKnowledgeBaseBytes {
		return Document{}, ErrLimitExceeded
	}
	if taken, err := s.repo.ChecksumTaken(ctx, kb.ID, checksum); err != nil {
		return Document{}, err
	} else if taken {
		return Document{}, ErrDuplicate
	}

	doc := Document{
		ID: id.New(), KnowledgeBaseID: kb.ID, Filename: filename, MIMEType: pdfMIMEType, SizeBytes: size,
		Checksum: checksum, Status: enums.DocumentStatusPending, UploadedByOperatorID: scope.OperatorID,
		PersonalDataTermsVersion: int16(up.TermsVersion),
	}
	doc.ObjectKey = ObjectKey(kb.ID, doc.ID)

	sealed, err := s.sealer.EncryptBytes(up.Data)
	if err != nil {
		return Document{}, fmt.Errorf("documents: encrypt the file: %w", err)
	}
	if err := s.objects.Put(ctx, doc.ObjectKey, sealed); err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrStorage, err)
	}

	var saved Document
	err = s.tx(ctx, func(tx Store) error {
		locked, err := tx.KnowledgeBaseForUpdate(ctx, agentID)
		if err != nil {
			return err
		}
		if locked.SizeBytesTotal+size > MaxKnowledgeBaseBytes {
			return ErrLimitExceeded
		}
		if taken, err := tx.ChecksumTaken(ctx, locked.ID, checksum); err != nil {
			return err
		} else if taken {
			return ErrDuplicate
		}
		if saved, err = tx.InsertDocument(ctx, doc); err != nil {
			return err
		}
		return tx.AddSize(ctx, locked.ID, size)
	})
	if err != nil {
		s.dropOrphan(doc.ObjectKey)
		return Document{}, err
	}

	s.log.Info("document uploaded", slog.String("document_id", saved.ID.String()), slog.String("ai_agent_id", agentID.String()),
		slog.Int64("size_bytes", size), slog.String("operator_id", scope.OperatorID.String()))
	return saved, nil
}

// List answers an agent that never uploaded with an empty list and zero bytes, which is what its base would say.
func (s *Service) List(ctx context.Context, scope Scope, agentID uuid.UUID) (Listing, error) {
	if err := s.agentExists(ctx, scope, agentID); err != nil {
		return Listing{}, err
	}
	kb, _, err := s.repo.KnowledgeBase(ctx, agentID)
	if err != nil {
		return Listing{}, err
	}
	items, err := s.repo.List(ctx, scope.AccountID, agentID)
	if err != nil {
		return Listing{}, err
	}
	return Listing{Items: items, SizeBytesTotal: kb.SizeBytesTotal}, nil
}

// Delete is ADR-0064, regla 9: the row is marked and its size given back to the base. The object stays in the store, and the
// worker takes the vectors out of Qdrant on its next sweep.
func (s *Service) Delete(ctx context.Context, scope Scope, agentID, documentID uuid.UUID) error {
	if err := s.agentExists(ctx, scope, agentID); err != nil {
		return err
	}
	var removed uuid.UUID
	err := s.tx(ctx, func(tx Store) error {
		// The base first and the document second, the order Upload takes them in, so the two never wait on each other.
		locked, err := tx.KnowledgeBaseForUpdate(ctx, agentID)
		if errors.Is(err, errNoKnowledgeBase) {
			return ErrDocumentNotFound
		}
		if err != nil {
			return err
		}
		doc, err := tx.DocumentForUpdate(ctx, scope.AccountID, agentID, documentID)
		if err != nil {
			return err
		}
		if err := tx.SoftDeleteDocument(ctx, doc.ID); err != nil {
			return err
		}
		if err := tx.AddSize(ctx, locked.ID, -doc.SizeBytes); err != nil {
			return err
		}
		removed = doc.ID
		return nil
	})
	if err != nil {
		return err
	}
	// After the commit and not inside the transaction: a commit that fails would leave a log of a removal that never happened.
	s.log.Info("document removed", slog.String("document_id", removed.String()), slog.String("operator_id", scope.OperatorID.String()))
	return nil
}

func (s *Service) agentExists(ctx context.Context, scope Scope, agentID uuid.UUID) error {
	exists, err := s.repo.AgentExists(ctx, scope.AccountID, agentID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrAgentNotFound
	}
	return nil
}

// dropOrphan is ADR-0064, regla 6 (d): one attempt, and if it fails too, the key goes to the log, because nothing sweeps orphans.
// It does not take the request's context: a cancelled request may be why the transaction failed, and it would cancel this too.
func (s *Service) dropOrphan(key string) {
	if err := s.objects.Delete(context.Background(), key); err != nil {
		s.log.Error("orphan object left in the store", slog.String("object_key", key), slog.String("error", err.Error()))
	}
}

// isPDF asks both the declared type and the first bytes, because either one alone is what the client says (ADR-0064, regla 10).
func isPDF(up Upload) bool {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(up.MIMEType, ";")[0]))
	return mime == pdfMIMEType && bytes.HasPrefix(up.Data, pdfSignature)
}

// cleanFilename keeps the name the admin sees in the list and nothing of the path a browser may send with it. The name comes from
// the multipart header, so it is checked like any input: PostgreSQL refuses invalid UTF-8 and a NUL, and it would refuse them
// after the file was already uploaded.
func cleanFilename(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrInvalidFilename
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if utf8.RuneCountInString(name) > maxFilenameRunes {
		return "", ErrInvalidFilename
	}
	if name == "" {
		return "documento.pdf", nil
	}
	return name, nil
}
