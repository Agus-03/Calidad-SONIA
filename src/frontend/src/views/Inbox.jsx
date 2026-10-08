import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router';
import { ContactPanel } from '../features/contact/ContactPanel';
import { api } from '../lib/api';
import { IconChat, IconUser, IconHistory } from '../components/icons';
import isotipoDark from '../assets/isotipo-dark.svg';
import './Inbox.css';

const mockConversations = [
  { id: '1', name: 'Juan Pérez', lastMessage: 'Hola, necesito ayuda con...', time: '15:40', avatar: 'J', muted: true },
  { id: '2', name: 'María Gómez', lastMessage: 'Gracias por la info.', time: '15:39', avatar: 'M', muted: false },
  { id: '3', name: 'Carlos López', lastMessage: '¿A qué hora abren?', time: '15:16', avatar: 'C', muted: true },
  { id: '4', name: 'Soporte Técnico', lastMessage: 'Su ticket ha sido resuelto.', time: '15:11', avatar: 'S', muted: true },
  { id: '5', name: 'Ana Silva', lastMessage: 'Perfecto, nos vemos.', time: '14:49', avatar: 'A', muted: false },
];

export default function Inbox() {
  const navigate = useNavigate();
  const [conversations, setConversations] = useState(mockConversations);
  const [selectedConversation, setSelectedConversation] = useState(null);

  const mockContactId = '123e4567-e89b-12d3-a456-426614174001';

  return (
    <div className="inbox-layout">
      {/* Sidebar for Navigation */}
      <div className="inbox-sidebar">
        <div className="sidebar-logo" title="SONIA">
          <img src={isotipoDark} alt="SONIA" />
        </div>
        <nav>
          <a href="#" className="active" title="Bandeja de entrada"><IconChat size={24} /></a>
          <a href="#" title="Contactos"><IconUser size={24} /></a>
          <a href="#" title="Reportes"><IconHistory size={24} /></a>
        </nav>
      </div>

      {/* List of Conversations */}
      <div className="inbox-list">
        <div className="list-header-top">
          <h2>Bandeja de entrada</h2>
          <div className="header-actions">
            <button className="icon-btn">⋮</button>
            <button className="icon-btn" onClick={() => navigate('/inboxes/new')} title="Add WhatsApp Channel">+</button>
          </div>
        </div>

        <div className="search-container">
          <div className="search-bar">
            <span className="search-icon">
              <svg viewBox="0 0 24 24" height="20" width="20" preserveAspectRatio="xMidYMid meet" version="1.1" x="0px" y="0px" enableBackground="new 0 0 24 24" xmlSpace="preserve"><path fill="currentColor" d="M15.009,13.805h-0.636l-0.22-0.219c0.781-0.911,1.256-2.092,1.256-3.386 c0-2.876-2.332-5.207-5.207-5.207c-2.876,0-5.208,2.331-5.208,5.207s2.331,5.208,5.208,5.208c1.293,0,2.474-0.474,3.385-1.255 l0.221,0.22v0.635l4.004,3.999l1.194-1.195L15.009,13.805Z M10.201,13.805c-1.991,0-3.605-1.614-3.605-3.605 s1.614-3.605,3.605-3.605s3.605,1.614,3.605,3.605S12.192,13.805,10.201,13.805Z"></path></svg>
            </span>
            <input type="text" placeholder="Buscar un chat o iniciar uno nuevo" />
          </div>
        </div>

        <div className="filter-chips">
          <button className="chip active">Todos</button>
          <button className="chip">No leídos</button>
          <button className="chip">Favoritos</button>
          <button className="chip">Grupos</button>
          <button className="chip">+</button>
        </div>

        <div className="list-items">
          {conversations.map(conv => (
            <div
              key={conv.id}
              className={`list-item ${selectedConversation === conv.id ? 'selected' : ''}`}
              onClick={() => setSelectedConversation(conv.id)}
            >
              <div className="item-avatar">{conv.avatar}</div>
              <div className="item-content">
                <div className="item-row">
                  <h4>{conv.name}</h4>
                  <span className="item-time">{conv.time}</span>
                </div>
                <div className="item-row">
                  <p>{conv.lastMessage}</p>
                  {conv.muted && (
                    <span className="item-muted">
                      <svg viewBox="0 0 24 24" height="18" width="18" preserveAspectRatio="xMidYMid meet" className="" version="1.1" x="0px" y="0px" enableBackground="new 0 0 24 24" xmlSpace="preserve"><path fill="currentColor" d="M12,4.8c1.6,0,2.9,1.3,2.9,2.9v4.2l3.4,3.4c0.2,0.2,0.3,0.5,0.2,0.8 c-0.1,0.3-0.4,0.5-0.7,0.5H5.8c-0.3,0-0.6-0.2-0.7-0.5c-0.1-0.3,0-0.6,0.2-0.8l3.4-3.4V7.7C8.7,6.1,10,4.8,11.6,4.8 M12,3 c-2.6,0-4.7,2.1-4.7,4.7v3.8L4.4,14.4c-0.6,0.6-0.7,1.5-0.3,2.2c0.4,0.7,1.2,1.2,2,1.2h11.4c0.8,0,1.5-0.4,2-1.2 c0.4-0.8,0.3-1.7-0.3-2.2l-2.9-2.9V7.7C16.4,5.1,14.3,3,11.7,3L12,3z"></path><path fill="currentColor" d="M11.8,20.8c-1.3,0-2.4-1.1-2.4-2.4h4.8C14.2,19.7,13.1,20.8,11.8,20.8z"></path><rect x="4.9" y="10.8" transform="matrix(0.7071 -0.7071 0.7071 0.7071 -6.0402 11.8021)" fill="currentColor" width="13.9" height="1.8"></rect></svg>
                    </span>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {/* Main Chat Area */}
      <div className="inbox-chat">
        {selectedConversation ? (
          <div className="chat-content">
            <div className="chat-header">
              <div className="item-avatar" style={{ width: 40, height: 40, marginRight: 16 }}>
                {conversations.find(c => c.id === selectedConversation)?.avatar}
              </div>
              <h3>{conversations.find(c => c.id === selectedConversation)?.name}</h3>
            </div>
            <div className="chat-messages">
              <div className="message incoming">Hola, necesito ayuda con mi factura.</div>
              <div className="message outgoing">¡Hola! Claro, ¿me puedes indicar el número de factura?</div>
            </div>
          </div>
        ) : (
          <div className="empty-chat">
            <p>Selecciona una conversación para empezar</p>
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
