import React from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router';
import { ToastProvider } from './components/Toast';
import Login from './views/Login';
import Inbox from './views/Inbox';
import CreateWhatsAppChannel from './views/CreateWhatsAppChannel';

function App() {
  return (
    <ToastProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/login" replace />} />
          <Route path="/login" element={<Login />} />
          <Route path="/inbox" element={<Inbox />} />
          <Route path="/inboxes/new" element={<CreateWhatsAppChannel />} />
        </Routes>
      </BrowserRouter>
    </ToastProvider>
  );
}

export default App;
