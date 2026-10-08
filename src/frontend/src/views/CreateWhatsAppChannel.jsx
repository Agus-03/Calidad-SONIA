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
      await api.post('/inboxes', {
        inbox_name: formData.inboxName,
        phone_number: formData.phoneNumber,
        phone_number_id: formData.phoneNumberId,
        business_account_id: formData.businessAccountId,
        api_key: formData.apiKey,
      });
      // Optionally navigate to step 3 (Add Agents) when prepared
      // navigate('/inboxes/add-agents');
      console.log('Inbox created successfully');
    } catch (err) {
      console.error(err);
      // Fallback error handling
      setErrors({ form: 'An error occurred while creating the channel.' });
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
            
            <div className="step active">
              <div className="step-indicator">2</div>
              <div className="step-content">
                <h3>Create Inbox</h3>
                <p>Authenticate your account and create an inbox.</p>
              </div>
            </div>

            <div className="step-connector"></div>

            <div className="step pending">
              <div className="step-indicator">3</div>
              <div className="step-content">
                <h3>Add Agents</h3>
                <p>Add agents to the created inbox.</p>
              </div>
            </div>

            <div className="step-connector"></div>

            <div className="step pending">
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
        </div>
      </div>
    </div>
  );
}
