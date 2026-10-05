import React, { useState, useEffect } from 'react';
import { ContactPanel } from '../features/contact/ContactPanel';
import { api } from '../lib/api';
import './Inbox.css';

export default function Inbox() {
  const [conversations, setConversations] = useState([]);
  const [selectedConversation, setSelectedConversation] = useState(null);

  // Use a mocked contact since the backend might not be up or we might not have data
  const mockConversationId = '123e4567-e89b-12d3-a456-426614174000';
  const mockContactId = '123e4567-e89b-12d3-a456-426614174001';

  return (
    <div className="inbox-layout">
      {/* Sidebar for Navigation (Mock) */}
      <div className="inbox-sidebar">
        <div className="sidebar-logo">SONIA</div>
        <nav>
          <a href="#" className="active">Bandeja de entrada</a>
          <a href="#">Contactos</a>
          <a href="#">Reportes</a>
        </nav>
      </div>

      {/* List of Conversations (Mock) */}
      <div className="inbox-list">
        <div className="list-header">
          <h2>Conversaciones</h2>
        </div>
        <div className="list-items">
          <div 
            className={`list-item ${selectedConversation === mockConversationId ? 'selected' : ''}`}
            onClick={() => setSelectedConversation(mockConversationId)}
          >
            <div className="item-avatar">J</div>
            <div className="item-details">
              <h4>Juan Pérez</h4>
              <p>Hola, necesito ayuda con...</p>
            </div>
          </div>
        </div>
      </div>

      {/* Main Chat Area (Mock) */}
      <div className="inbox-chat">
        {selectedConversation ? (
          <div className="chat-content">
            <div className="chat-header">
              <h3>Juan Pérez</h3>
            </div>
            <div className="chat-messages">
              <div className="message incoming">Hola, necesito ayuda con mi factura.</div>
              <div className="message outgoing">¡Hola Juan! Claro, ¿me puedes indicar el número de factura?</div>
            </div>
          </div>
        ) : (
          <div className="empty-chat">
            <p>Selecciona una conversación para comenzar</p>
          </div>
        )}
      </div>

      {/* Contact Panel */}
      <ContactPanel 
        contactId={selectedConversation ? mockContactId : null} 
        conversationId={selectedConversation} 
      />
    </div>
  );
}
