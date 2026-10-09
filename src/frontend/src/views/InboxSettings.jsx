import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { api } from '../lib/api';
import './InboxSettings.css';

export default function InboxSettings() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [inbox, setInbox] = useState(null);
  const [isLoading, setIsLoading] = useState(true);
  const [newApiKey, setNewApiKey] = useState('');
  const [isUpdatingKey, setIsUpdatingKey] = useState(false);

  useEffect(() => {
    fetchInboxDetails();
  }, [id]);

  const fetchInboxDetails = async () => {
    setIsLoading(true);
    try {
      const response = await api.get('/inboxes');
      const found = (response.items || []).find((i) => i.id === id);
      if (found) {
        setInbox(found);
      } else {
        console.error('Inbox not found');
        navigate('/inboxes');
      }
    } catch (err) {
      console.error('Failed to fetch inbox details:', err);
    } finally {
      setIsLoading(false);
    }
  };

  const handleUpdateApiKey = async () => {
    if (!newApiKey.trim()) return;
    setIsUpdatingKey(true);
    try {
      // NOTE: This endpoint might not exist yet on the backend.
      // await api.patch(`/inboxes/${id}/api-key`, { api_key: newApiKey.trim() });
      alert('Clave de API actualizada correctamente (Simulado).');
      setNewApiKey('');
    } catch (err) {
      console.error('Failed to update API Key:', err);
      alert('Error al actualizar la clave.');
    } finally {
      setIsUpdatingKey(false);
    }
  };

  if (isLoading) {
    return <div className="settings-loading">Cargando configuración...</div>;
  }

  return (
    <div className="inbox-settings-container">
      <div className="settings-nav">
        <button className="btn-back-nav" onClick={() => navigate('/inboxes')}>
          &lt; Atrás
        </button>
        <span className="nav-title">Entradas</span>
      </div>

      <div className="settings-header">
        <h2>{inbox?.display_name || 'Test Number WhatsApp'}</h2>
        <p className="phone-number">+{inbox?.phone_number_id || '15556531059'}</p>
      </div>

      <div className="settings-content">
        <div className="setting-row">
          <div className="setting-info">
            <label>Actualizar Clave API</label>
          </div>
          <div className="setting-control update-control">
            <input
              type="text"
              placeholder="Introduzca aquí la nueva Clave API"
              value={newApiKey}
              onChange={(e) => setNewApiKey(e.target.value)}
            />
            <button
              className="btn-update"
              onClick={handleUpdateApiKey}
              disabled={isUpdatingKey || !newApiKey.trim()}
            >
              Actualizar
            </button>
            <p className="help-text">Introduzca la clave actualizada para la integración con las API de WhatsApp.</p>
          </div>
        </div>
      </div>
    </div>
  );
}
