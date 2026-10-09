import React, { useState, useEffect } from 'react';
import { api } from '../../lib/api';
import { useToast } from '../../components/Toast';
import { Skeleton } from '../../components/Feedback';
import { IconPhone, IconMail, IconCalendar, IconEdit, IconCheckCircle, IconTrash } from '../../components/icons';
import { formatPhone, formatDate } from '../../lib/format';
import { TagsSection } from './TagsSection';
import { CustomAttributes } from './CustomAttributes';
import './ContactPanel.css';

function ProfileHeader({ contact, loading }) {
  if (loading) {
    return (
      <div className="profile-header skeleton-container">
        <div className="profile-avatar"><Skeleton width="100%" height="100%" borderRadius="50%" /></div>
        <div className="profile-details">
          <Skeleton width="150px" height="24px" />
          <Skeleton width="100px" height="16px" />
        </div>
      </div>
    );
  }

  const initial = contact.name ? contact.name.charAt(0).toUpperCase() : '?';

  return (
    <div className="profile-header">
      <div className="profile-avatar">{initial}</div>
      <div className="profile-details">
        <h2 className="profile-name">{contact.name || 'Desconocido'}</h2>
        <div className="profile-phone">
          <IconPhone />
          <span>{formatPhone(contact.phone)}</span>
        </div>
      </div>
    </div>
  );
}

function StandardInfo({ contact, loading }) {
  if (loading) {
    return (
      <div className="standard-info skeleton-container">
        <Skeleton width="100%" height="20px" />
        <Skeleton width="100%" height="20px" />
      </div>
    );
  }

  return (
    <div className="standard-info">
      {contact.email && (
        <div className="info-row">
          <span className="info-icon"><IconMail /></span>
          <span className="info-value">{contact.email}</span>
        </div>
      )}
      <div className="info-row">
        <span className="info-icon"><IconCalendar /></span>
        <span className="info-value">Creado: {formatDate(contact.created_at)}</span>
      </div>
    </div>
  );
}

function QuickActions({ contactId, conversationId }) {
  const { addToast } = useToast();

  const handleEdit = () => {
    addToast('Editar contacto: Próximamente', 'info');
  };

  const handleResolve = async () => {
    if (!conversationId) return;
    try {
      await api.post(`/conversations/${conversationId}/resolve`);
      addToast('Conversación resuelta', 'success');
    } catch (err) {
      addToast('Error al resolver la conversación', 'error');
    }
  };

  return (
    <div className="quick-actions">
      <button className="action-btn" onClick={handleEdit} title="Editar contacto">
        <IconEdit />
      </button>
      {conversationId && (
        <button className="action-btn success" onClick={handleResolve} title="Resolver conversación">
          <IconCheckCircle />
        </button>
      )}
    </div>
  );
}

function BottomTabs({ conversationId }) {
  const [activeTab, setActiveTab] = useState('notes');
  const [notes, setNotes] = useState([]);
  const [loadingNotes, setLoadingNotes] = useState(false);
  const [newNote, setNewNote] = useState('');
  const [savingNote, setSavingNote] = useState(false);
  const { addToast } = useToast();

  useEffect(() => {
    if (activeTab === 'notes' && conversationId) {
      loadNotes();
    }
  }, [activeTab, conversationId]);

  const loadNotes = async () => {
    setLoadingNotes(true);
    try {
      const data = await api.get(`/conversations/${conversationId}/notes`);
      setNotes(data.notes || []);
    } catch (err) {
      addToast('Error al cargar notas', 'error');
    } finally {
      setLoadingNotes(false);
    }
  };

  const handleAddNote = async () => {
    if (!newNote.trim()) return;
    setSavingNote(true);
    try {
      const added = await api.post(`/conversations/${conversationId}/notes`, { text: newNote });
      setNotes([...notes, added]);
      setNewNote('');
    } catch (err) {
      addToast('Error al guardar nota', 'error');
    } finally {
      setSavingNote(false);
    }
  };

  const handleDeleteNote = async (noteId) => {
    try {
      await api.delete(`/conversations/${conversationId}/notes/${noteId}`);
      setNotes(notes.filter((n) => n.id !== noteId));
    } catch (err) {
      addToast('Error al eliminar nota', 'error');
    }
  };

  return (
    <div className="bottom-tabs">
      <div className="tabs-header">
        <button 
          className={`tab-btn ${activeTab === 'notes' ? 'active' : ''}`}
          onClick={() => setActiveTab('notes')}
        >
          Notas Privadas
        </button>
        <button 
          className={`tab-btn ${activeTab === 'history' ? 'active' : ''}`}
          onClick={() => setActiveTab('history')}
        >
          Historial
        </button>
      </div>

      <div className="tabs-content">
        {activeTab === 'notes' && (
          <div className="notes-tab">
            <div className="notes-list">
              {loadingNotes ? (
                <Skeleton height="60px" count={3} />
              ) : notes.length === 0 ? (
                <p className="no-notes">No hay notas en esta conversación.</p>
              ) : (
                notes.map((note) => (
                  <div key={note.id} className="note-item">
                    <div className="note-text">{note.text}</div>
                    <div className="note-footer">
                      <span className="note-date">{formatDate(note.created_at)}</span>
                      <button className="delete-note-btn" onClick={() => handleDeleteNote(note.id)}>
                        <IconTrash />
                      </button>
                    </div>
                  </div>
                ))
              )}
            </div>
            <div className="note-input-area">
              <textarea
                placeholder="Escribe una nota privada..."
                value={newNote}
                onChange={(e) => setNewNote(e.target.value)}
                disabled={savingNote}
              />
              <button 
                className="btn-primary" 
                onClick={handleAddNote} 
                disabled={!newNote.trim() || savingNote}
              >
                {savingNote ? 'Guardando...' : 'Guardar'}
              </button>
            </div>
          </div>
        )}
        {activeTab === 'history' && (
          <div className="history-tab">
            <p className="placeholder-text">El historial de interacciones estará disponible próximamente.</p>
          </div>
        )}
      </div>
    </div>
  );
}

export function ContactPanel({ contactId, conversationId }) {
  const [contact, setContact] = useState(null);
  const [loading, setLoading] = useState(true);
  const { addToast } = useToast();

  useEffect(() => {
    if (!contactId) return;

    const fetchContact = async () => {
      setLoading(true);
      try {
        const data = await api.get(`/contacts/${contactId}`);
        setContact(data);
      } catch (err) {
        addToast('Error al cargar el contacto', 'error');
      } finally {
        setLoading(false);
      }
    };

    fetchContact();
  }, [contactId, addToast]);

  if (!contactId) {
    return (
      <div className="contact-panel empty-state">
        <p>Selecciona una conversación para ver los detalles del contacto.</p>
      </div>
    );
  }

  return (
    <div className="contact-panel">
      <div className="contact-panel-scrollable">
        <ProfileHeader contact={contact || {}} loading={loading} />
        
        {!loading && <QuickActions contactId={contactId} conversationId={conversationId} />}

        <div className="panel-section">
          <StandardInfo contact={contact || {}} loading={loading} />
        </div>

        <div className="panel-section">
          <h3>Etiquetas</h3>
          {loading ? <Skeleton height="30px" /> : (
            <TagsSection contactId={contactId} initialLabels={contact?.labels || []} />
          )}
        </div>

        <div className="panel-section">
          <h3>Atributos</h3>
          {loading ? <Skeleton height="50px" /> : (
            <CustomAttributes contactId={contactId} initialAttributes={contact?.attributes || []} />
          )}
        </div>
      </div>
      
      {conversationId && (
        <div className="contact-panel-footer">
          <BottomTabs conversationId={conversationId} />
        </div>
      )}
    </div>
  );
}
