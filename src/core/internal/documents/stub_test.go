package documents

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// stubStore answers from memory, so the service is tested against its own rules and not against the SQL that enforces them.
type stubStore struct {
	mu        sync.Mutex
	accountID uuid.UUID
	agents    map[uuid.UUID]bool
	bases     map[uuid.UUID]KnowledgeBase // by agent
	docs      map[uuid.UUID]Document
	// Set to make the insert fail, which is how a test reaches the orphan cleanup of ADR-0064, regla 6 (d).
	insertErr error
	// Set to make every read fail, which is how a test reaches the branches that let an unknown error through.
	readErr error
	// Called inside the transaction before the lock, so a test can play the upload that won the race.
	beforeLock func(*stubStore)
}

func newStubStore(accountID uuid.UUID, agents ...uuid.UUID) *stubStore {
	s := &stubStore{accountID: accountID, agents: map[uuid.UUID]bool{}, bases: map[uuid.UUID]KnowledgeBase{}, docs: map[uuid.UUID]Document{}}
	for _, a := range agents {
		s.agents[a] = true
	}
	return s
}

// tx rolls back what fn wrote when it fails, as the database would.
func (s *stubStore) tx(_ context.Context, fn func(Store) error) error {
	s.mu.Lock()
	bases, docs := maps.Clone(s.bases), maps.Clone(s.docs)
	s.mu.Unlock()
	if err := fn(s); err != nil {
		s.mu.Lock()
		s.bases, s.docs = bases, docs
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *stubStore) AgentExists(_ context.Context, accountID, agentID uuid.UUID) (bool, error) {
	if s.readErr != nil {
		return false, s.readErr
	}
	return accountID == s.accountID && s.agents[agentID], nil
}

func (s *stubStore) EnsureKnowledgeBase(_ context.Context, kb KnowledgeBase) (KnowledgeBase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.bases[kb.AIAgentID]; ok {
		return existing, nil
	}
	s.bases[kb.AIAgentID] = kb
	return kb, nil
}

func (s *stubStore) KnowledgeBase(_ context.Context, agentID uuid.UUID) (KnowledgeBase, bool, error) {
	if s.readErr != nil {
		return KnowledgeBase{}, false, s.readErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kb, ok := s.bases[agentID]
	return kb, ok, nil
}

func (s *stubStore) KnowledgeBaseForUpdate(_ context.Context, agentID uuid.UUID) (KnowledgeBase, error) {
	if s.beforeLock != nil {
		race := s.beforeLock
		s.beforeLock = nil
		race(s)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kb, ok := s.bases[agentID]
	if !ok {
		return KnowledgeBase{}, errNoKnowledgeBase
	}
	return kb, nil
}

func (s *stubStore) AddSize(_ context.Context, knowledgeBaseID uuid.UUID, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for agent, kb := range s.bases {
		if kb.ID == knowledgeBaseID {
			kb.SizeBytesTotal += delta
			s.bases[agent] = kb
		}
	}
	return nil
}

func (s *stubStore) ChecksumTaken(_ context.Context, knowledgeBaseID uuid.UUID, checksum string) (bool, error) {
	if s.readErr != nil {
		return false, s.readErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.docs {
		if d.KnowledgeBaseID == knowledgeBaseID && d.Checksum == checksum {
			return true, nil
		}
	}
	return false, nil
}

func (s *stubStore) InsertDocument(_ context.Context, d Document) (Document, error) {
	if s.insertErr != nil {
		return Document{}, s.insertErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d.CreatedAt = time.Date(2026, 10, 3, 12, 0, len(s.docs), 0, time.UTC)
	s.docs[d.ID] = d
	return d, nil
}

func (s *stubStore) List(_ context.Context, accountID, agentID uuid.UUID) ([]Document, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := []Document{}
	kb, ok := s.bases[agentID]
	if accountID != s.accountID || !ok {
		return found, nil
	}
	for _, d := range s.docs {
		if d.KnowledgeBaseID == kb.ID {
			found = append(found, d)
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].CreatedAt.After(found[j].CreatedAt) })
	return found, nil
}

func (s *stubStore) DocumentForUpdate(_ context.Context, accountID, agentID, documentID uuid.UUID) (Document, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[documentID]
	kb := s.bases[agentID]
	if !ok || accountID != s.accountID || d.KnowledgeBaseID != kb.ID {
		return Document{}, ErrDocumentNotFound
	}
	return d, nil
}

// Removed rows leave the map: the stub has no deleted_at, and every read of the service looks at live documents only.
func (s *stubStore) SoftDeleteDocument(_ context.Context, documentID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.docs[documentID]; !ok {
		return ErrDocumentNotFound
	}
	delete(s.docs, documentID)
	return nil
}

// stubObjects is the bucket: what was put, and what was asked to go.
type stubObjects struct {
	mu        sync.Mutex
	objects   map[string][]byte
	deleted   []string
	putErr    error
	deleteErr error
}

func newStubObjects() *stubObjects { return &stubObjects{objects: map[string][]byte{}} }

func (o *stubObjects) Put(_ context.Context, key string, data []byte) error {
	if o.putErr != nil {
		return o.putErr
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = bytes.Clone(data)
	return nil
}

func (o *stubObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deleted = append(o.deleted, key)
	if o.deleteErr != nil {
		return o.deleteErr
	}
	delete(o.objects, key)
	return nil
}

// stubSealer marks what it sealed, so a test can tell the object is the sealed file and not the PDF.
type stubSealer struct{ err error }

func (s stubSealer) EncryptBytes(plaintext []byte) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte("sealed:"), plaintext...), nil
}

var errBoom = errors.New("documents: the stub was told to fail")

func pdf(body string) []byte { return []byte("%PDF-1.7\n" + body) }

func validUpload(body string) Upload {
	return Upload{Filename: "precios.pdf", MIMEType: "application/pdf", Data: pdf(body), TermsVersion: NoticeVersion}
}
