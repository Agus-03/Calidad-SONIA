import React, { useState } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../lib/api';
import './CreateWhatsAppChannel.css';

export default function CreateWhatsAppChannel() {
  const navigate = useNavigate();
  const [formData, setFormData] = useState({
    inboxName: '',
    phoneNumber: '',
    phoneNumberId: '',
    businessAccountId: '',
    apiKey: '',
  });

  const [step, setStep] = useState(2);
  const [createdInboxId, setCreatedInboxId] = useState(null);
  
  // Agents State (Step 3)
  const [availableAgents, setAvailableAgents] = useState([]);
  const [selectedAgents, setSelectedAgents] = useState([]);
  const [agentSearchTerm, setAgentSearchTerm] = useState('');
  const [isAgentsLoading, setIsAgentsLoading] = useState(false);
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);

  const [errors, setErrors] = useState({});
  const [isSubmitting, setIsSubmitting] = useState(false);

  const validateForm = () => {
    const newErrors = {};
    if (!formData.inboxName.trim()) newErrors.inboxName = 'Please enter a valid value.';
    if (!formData.phoneNumber.trim()) newErrors.phoneNumber = 'Please enter a valid value.';
    if (!formData.phoneNumberId.trim()) newErrors.phoneNumberId = 'Please enter a valid value.';
    if (!formData.businessAccountId.trim()) newErrors.businessAccountId = 'Please enter a valid value.';
    if (!formData.apiKey.trim()) newErrors.apiKey = 'Please enter a valid value.';
    
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!validateForm()) return;

    setIsSubmitting(true);
    try {
      const response = await api.post('/inboxes', {
        inbox_name: formData.inboxName,
        phone_number: formData.phoneNumber,
        phone_number_id: formData.phoneNumberId,
        business_account_id: formData.businessAccountId,
        api_key: formData.apiKey,
      });
      setCreatedInboxId(response.id);
      fetchAgents();
      setStep(3);
    } catch (err) {
      console.error(err);
      if (err.code === 'phone_number_in_use') {
        setErrors({ phoneNumberId: 'This Phone number ID is already registered.' });
      } else if (err.code === 'invalid_credentials') {
        setErrors({ form: 'The provided Phone number ID or API key are invalid. Please check your Meta credentials.' });
      } else {
        setErrors({ form: 'An error occurred while creating the channel.' });
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const fetchAgents = async () => {
    setIsAgentsLoading(true);
    try {
      const response = await api.get('/agents');
      setAvailableAgents(response.items || []);
    } catch (err) {
      console.error('Failed to fetch agents', err);
    } finally {
      setIsAgentsLoading(false);
    }
  };

  const handleAgentSelect = (agent) => {
    if (!selectedAgents.find((a) => a.id === agent.id)) {
      setSelectedAgents([...selectedAgents, agent]);
    }
    setAgentSearchTerm('');
    setIsDropdownOpen(false);
  };

  const handleAgentRemove = (agentId) => {
    setSelectedAgents(selectedAgents.filter((a) => a.id !== agentId));
  };

  const handleAgentsSubmit = async (e) => {
    e.preventDefault();
    setIsSubmitting(true);
    try {
      const agentIds = selectedAgents.map((a) => a.id);
      await api.post(`/inboxes/${createdInboxId}/agents`, { agent_ids: agentIds });
      setStep(4);
    } catch (err) {
      console.error(err);
      setErrors({ form: 'An error occurred while assigning agents.' });
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => ({ ...prev, [name]: value }));
    // Clear error when user starts typing
    if (errors[name]) {
      setErrors((prev) => ({ ...prev, [name]: '' }));
    }
  };

  return (
    <div className="wizard-layout">
      {/* Top Header / Back Button */}
      <div className="wizard-header">
        <button className="btn-link back-btn" onClick={() => navigate('/inbox')}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 18 9 12 15 6"></polyline>
          </svg>
          Back Inboxes
        </button>
      </div>

      <div className="wizard-content">
        {/* Left Column: Stepper */}
        <div className="wizard-sidebar">
          <div className="stepper">
            <div className="step completed">
              <div className="step-indicator">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="20 6 9 17 4 12"></polyline>
                </svg>
              </div>
              <div className="step-content">
                <h3>Choose Channel</h3>
                <p>Choose the provider you want to integrate with Chatwoot.</p>
              </div>
            </div>
            
            <div className="step-connector"></div>
            
            <div className={`step ${step > 2 ? 'completed' : step === 2 ? 'active' : 'pending'}`}>
              <div className="step-indicator">
                {step > 2 ? (
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="20 6 9 17 4 12"></polyline>
                  </svg>
                ) : '2'}
              </div>
              <div className="step-content">
                <h3>Create Inbox</h3>
                <p>Authenticate your account and create an inbox.</p>
              </div>
            </div>

            <div className="step-connector"></div>

            <div className={`step ${step > 3 ? 'completed' : step === 3 ? 'active' : 'pending'}`}>
              <div className="step-indicator">
                {step > 3 ? (
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="20 6 9 17 4 12"></polyline>
                  </svg>
                ) : '3'}
              </div>
              <div className="step-content">
                <h3>Add Agents</h3>
                <p>Add agents to the created inbox.</p>
              </div>
            </div>

            <div className="step-connector"></div>

            <div className={`step ${step === 4 ? 'active' : 'pending'}`}>
              <div className="step-indicator">4</div>
              <div className="step-content">
                <h3>Voilà!</h3>
                <p>You are all set to go!</p>
              </div>
            </div>
          </div>
        </div>

        {/* Right Column: Form */}
        <div className="wizard-form-container">
          {step === 2 && (
          <form className="wizard-form" onSubmit={handleSubmit}>
            <div className="form-group">
              <label>Inbox Name</label>
              <div className="input-wrapper">
                <input
                  type="text"
                  name="inboxName"
                  value={formData.inboxName}
                  onChange={handleChange}
                  className={errors.inboxName ? 'error-input' : ''}
                />
              </div>
              {errors.inboxName && <span className="error-message">{errors.inboxName}</span>}
            </div>

            <div className="form-group">
              <label>Phone number</label>
              <div className="input-wrapper">
                <input
                  type="text"
                  name="phoneNumber"
                  value={formData.phoneNumber}
                  onChange={handleChange}
                  className={errors.phoneNumber ? 'error-input' : ''}
                />
              </div>
              {errors.phoneNumber && <span className="error-message">{errors.phoneNumber}</span>}
            </div>

            <div className="form-group">
              <label>Phone number ID</label>
              <div className="input-wrapper">
                <input
                  type="text"
                  name="phoneNumberId"
                  value={formData.phoneNumberId}
                  onChange={handleChange}
                  className={errors.phoneNumberId ? 'error-input' : ''}
                />
              </div>
              {errors.phoneNumberId && <span className="error-message">{errors.phoneNumberId}</span>}
            </div>

            <div className="form-group">
              <label>Business Account ID</label>
              <div className="input-wrapper">
                <input
                  type="text"
                  name="businessAccountId"
                  value={formData.businessAccountId}
                  onChange={handleChange}
                  className={errors.businessAccountId ? 'error-input' : ''}
                />
              </div>
              {errors.businessAccountId && <span className="error-message">{errors.businessAccountId}</span>}
            </div>

            <div className="form-group">
              <label>API key</label>
              <div className="input-wrapper">
                <input
                  type="password"
                  name="apiKey"
                  value={formData.apiKey}
                  onChange={handleChange}
                  placeholder="API key"
                  className={errors.apiKey ? 'error-input' : ''}
                />
              </div>
              {errors.apiKey && <span className="error-message">{errors.apiKey}</span>}
            </div>

            {errors.form && <div className="error-message form-error">{errors.form}</div>}

            <div className="form-actions">
              <button type="submit" className="btn-primary" disabled={isSubmitting}>
                {isSubmitting ? <span className="spinner"></span> : 'Create WhatsApp Channel'}
              </button>
            </div>
          </form>
          )}

          {step === 3 && (
            <form className="wizard-form" onSubmit={handleAgentsSubmit}>
              <div className="wizard-intro">
                <h2>Añadir agentes a tu bandeja de entrada</h2>
                <p>Selecciona los agentes que tendrán acceso a las conversaciones de este canal.</p>
              </div>
              
              <div className="form-group agent-selector">
                <label>Seleccionar Agentes</label>
                <div className="combobox-wrapper">
                  <input
                    type="text"
                    placeholder="Buscar agente por nombre..."
                    value={agentSearchTerm}
                    onChange={(e) => {
                      setAgentSearchTerm(e.target.value);
                      setIsDropdownOpen(true);
                    }}
                    onFocus={() => setIsDropdownOpen(true)}
                  />
                  {isDropdownOpen && (
                    <div className="dropdown-menu">
                      {isAgentsLoading ? (
                        <div className="dropdown-item">Cargando agentes...</div>
                      ) : availableAgents.length === 0 ? (
                        <div className="dropdown-item">No hay agentes disponibles.</div>
                      ) : (
                        availableAgents
                          .filter((a) => !selectedAgents.find(sa => sa.id === a.id))
                          .filter((a) => 
                            `${a.name} ${a.lastname}`.toLowerCase().includes(agentSearchTerm.toLowerCase()) ||
                            a.email?.toLowerCase().includes(agentSearchTerm.toLowerCase())
                          )
                          .map((agent) => (
                            <div 
                              key={agent.id} 
                              className="dropdown-item"
                              onClick={() => handleAgentSelect(agent)}
                            >
                              <div className="agent-option">
                                <span className="agent-name">{agent.name} {agent.lastname}</span>
                                <span className="agent-email">{agent.email}</span>
                              </div>
                            </div>
                          ))
                      )}
                    </div>
                  )}
                </div>
                
                {selectedAgents.length > 0 && (
                  <div className="selected-chips">
                    {selectedAgents.map((agent) => (
                      <div key={agent.id} className="chip">
                        <span>{agent.name} {agent.lastname}</span>
                        <button type="button" onClick={() => handleAgentRemove(agent.id)}>x</button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              {errors.form && <div className="error-message form-error">{errors.form}</div>}

              <div className="form-actions">
                <button type="submit" className="btn-primary" disabled={isSubmitting}>
                  {isSubmitting ? <span className="spinner"></span> : 'Añadir agentes'}
                </button>
              </div>
            </form>
          )}

          {step === 4 && (
            <div className="wizard-form success-state">
              <div className="success-icon">
                <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="#25D366" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path>
                  <polyline points="22 4 12 14.01 9 11.01"></polyline>
                </svg>
              </div>
              <h2>¡Tu bandeja de entrada está lista!</h2>
              <p>Ahora puedes colaborar con tus clientes y gestionar tus conversaciones de WhatsApp directamente desde Sonia.</p>
              
              <div className="form-actions">
                <button 
                  type="button" 
                  className="btn-primary" 
                  onClick={() => navigate('/inboxes')}
                >
                  Llévame allí
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
