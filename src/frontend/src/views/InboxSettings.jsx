import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { api } from '../lib/api';
import './InboxSettings.css';

export default function InboxSettings() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [inbox, setInbox] = useState(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);

  const [aiAgentId, setAiAgentId] = useState('');
  const [humanAgentIds, setHumanAgentIds] = useState('');

  useEffect(() => {
    fetchInboxDetails();
  }, [id]);

  const fetchInboxDetails = async () => {
    setIsLoading(true);
    try {
      // API doesn't expose GET /inboxes/:id, so we fetch all and find the one.
      const response = await api.get('/inboxes');
      const found = (response.items || []).find((i) => i.id === id);
      if (found) {
        setInbox(found);
        setAiAgentId(found.default_ai_agent_id || '');
        // For human agents we don't have the list returned in the list endpoint yet,
        // so it starts empty for now until they are provided by the backend.
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

  const handleSave = async (e) => {
    e.preventDefault();
    setIsSaving(true);
    try {
      // 1. Update AI Agent
      const aiPayload = { default_ai_agent_id: aiAgentId.trim() || null };
      await api.patch(`/inboxes/${id}`, aiPayload);

      // 2. Update Human Agents
      const agentsList = humanAgentIds
        .split(',')
        .map((a) => a.trim())
        .filter(Boolean);
      
      await api.put(`/inboxes/${id}/agents`, { agent_ids: agentsList });

      alert('Configuración guardada correctamente.');
      navigate('/inboxes');
    } catch (err) {
      console.error('Failed to save settings:', err);
      alert('Error al guardar la configuración: ' + (err.message || 'Error desconocido'));
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) {
    return <div className="settings-loading">Cargando configuración...</div>;
  }

  return (
    <div className="inbox-settings-container">
      <div className="settings-header">
        <button className="btn-back" onClick={() => navigate('/inboxes')}>
          &larr; Volver
        </button>
        <h2>Configuración de la Bandeja</h2>
        <p>Estás editando: <strong>{inbox?.display_name || 'Desconocido'}</strong> ({inbox?.phone_number_id})</p>
      </div>

      <div className="settings-content">
        <form className="settings-form" onSubmit={handleSave}>
          <div className="form-group">
            <label htmlFor="aiAgentId">Agente de IA por Defecto (UUID)</label>
            <input
              id="aiAgentId"
              type="text"
              placeholder="Ej: 123e4567-e89b-12d3-a456-426614174000"
              value={aiAgentId}
              onChange={(e) => setAiAgentId(e.target.value)}
            />
            <span className="help-text">
              Dejalo vacío para no asignar ningún Agente de IA automático.
            </span>
          </div>

          <div className="form-group">
            <label htmlFor="humanAgentIds">Operadores Humanos (UUIDs separados por coma)</label>
            <input
              id="humanAgentIds"
              type="text"
              placeholder="Ej: uuid1, uuid2"
              value={humanAgentIds}
              onChange={(e) => setHumanAgentIds(e.target.value)}
            />
            <span className="help-text">
              Lista de IDs de operadores que tendrán acceso a esta bandeja.
            </span>
          </div>

          <div className="form-actions">
            <button type="button" className="btn-secondary" onClick={() => navigate('/inboxes')}>
              Cancelar
            </button>
            <button type="submit" className="btn-primary" disabled={isSaving}>
              {isSaving ? 'Guardando...' : 'Guardar Cambios'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
