import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../lib/api';
import './InboxesList.css';

export default function InboxesList() {
  const navigate = useNavigate();
  const [inboxes, setInboxes] = useState([]);
  const [searchTerm, setSearchTerm] = useState('');
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    fetchInboxes();
  }, []);

  const fetchInboxes = async () => {
    setIsLoading(true);
    try {
      const response = await api.get('/inboxes');
      setInboxes(response.items || []);
    } catch (err) {
      console.error('Failed to fetch inboxes:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const handleDelete = async (id) => {
    if (!window.confirm('¿Estás seguro de que quieres eliminar esta bandeja de entrada?')) return;
    try {
      await api.del(`/inboxes/${id}`);
      setInboxes(inboxes.filter((i) => i.id !== id));
    } catch (err) {
      console.error('Failed to delete inbox:', err);
    }
  };

  const filteredInboxes = inboxes.filter((inbox) =>
    inbox.display_name?.toLowerCase().includes(searchTerm.toLowerCase()) ||
    inbox.phone_number_id?.includes(searchTerm)
  );

  return (
    <div className="inboxes-list-container">
      <div className="inboxes-header">
        <div className="inboxes-title-section">
          <h2>Bandejas de entrada ({inboxes.length})</h2>
          <p>Administra tus bandejas de entrada y canales de comunicación conectados</p>
        </div>
        <div className="inboxes-actions">
          <button className="btn-primary" onClick={() => navigate('/inboxes/new')}>
            + Añadir bandeja de entrada
          </button>
        </div>
      </div>

      <div className="inboxes-search">
        <div className="search-wrapper">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <circle cx="11" cy="11" r="8"></circle>
            <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
          </svg>
          <input
            type="text"
            placeholder="Buscar bandejas de entrada..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
          />
        </div>
      </div>

      <div className="inboxes-content">
        {isLoading ? (
          <div className="loading-state">Cargando...</div>
        ) : filteredInboxes.length === 0 ? (
          <div className="empty-state">
            <p>No se encontraron bandejas de entrada.</p>
          </div>
        ) : (
          <div className="inboxes-grid">
            {filteredInboxes.map((inbox) => (
              <div key={inbox.id} className="inbox-card">
                <div className="inbox-card-header">
                  <div className="inbox-icon whatsapp">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor">
                      <path d="M17.472 14.382c-.297-.149-1.758-.867-2.03-.967-.273-.099-.471-.148-.67.15-.197.297-.767.966-.94 1.164-.173.199-.347.223-.644.075-.297-.15-1.255-.463-2.39-1.475-.883-.788-1.48-1.761-1.653-2.059-.173-.297-.018-.458.13-.606.134-.133.298-.347.446-.52.149-.174.198-.298.298-.497.099-.198.05-.371-.025-.52-.075-.149-.669-1.612-.916-2.207-.242-.579-.487-.5-.669-.51a12.8 12.8 0 0 0-.57-.01c-.198 0-.52.074-.792.372-.272.297-1.04 1.016-1.04 2.479 0 1.462 1.065 2.875 1.213 3.074.149.198 2.096 3.2 5.077 4.487.709.306 1.262.489 1.694.625.712.227 1.36.195 1.871.118.571-.085 1.758-.719 2.006-1.413.248-.694.248-1.289.173-1.413-.074-.124-.272-.198-.57-.347m-5.421 7.403h-.004a9.87 9.87 0 0 1-5.031-1.378l-.361-.214-3.741.982.998-3.648-.235-.374a9.86 9.86 0 0 1-1.51-5.26c.001-5.45 4.436-9.884 9.888-9.884 2.64 0 5.122 1.03 6.988 2.898a9.825 9.825 0 0 1 2.893 6.994c-.003 5.45-4.437 9.884-9.885 9.884m8.413-18.297A11.815 11.815 0 0 0 12.05 0C5.495 0 .16 5.335.157 11.892c0 2.096.547 4.142 1.588 5.945L.057 24l6.305-1.654a11.882 11.882 0 0 0 5.683 1.448h.005c6.554 0 11.89-5.335 11.893-11.893a11.821 11.821 0 0 0-3.48-8.413Z"/>
                    </svg>
                  </div>
                  <div className="inbox-details">
                    <h3>{inbox.display_name}</h3>
                    <span className="inbox-type">WhatsApp Cloud API</span>
                  </div>
                </div>
                <div className="inbox-card-body">
                  <div className="info-row">
                    <span className="info-label">Teléfono ID:</span>
                    <span className="info-value">{inbox.phone_number_id}</span>
                  </div>
                  <div className="info-row">
                    <span className="info-label">Estado:</span>
                    <span className={`status-badge ${inbox.status}`}>
                      {inbox.status}
                    </span>
                  </div>
                </div>
                <div className="inbox-card-actions">
                  <button className="btn-icon" title="Configuración" onClick={() => navigate(`/inboxes/${inbox.id}/settings`)}>
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <circle cx="12" cy="12" r="3"></circle>
                      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
                    </svg>
                    Settings
                  </button>
                  <button className="btn-icon text-danger" title="Eliminar" onClick={() => handleDelete(inbox.id)}>
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <polyline points="3 6 5 6 21 6"></polyline>
                      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    </svg>
                    Delete
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
