# SONIA - Sistema de Orquestación Conversacional con Composición Dinámica de Prompts

[![Status](https://img.shields.io/badge/Status-En%20Desarrollo-yellow)](#)
[![Integración](https://img.shields.io/badge/WhatsApp-Business%20Cloud%20API-25D366?logo=whatsapp&logoColor=white)](#)
[![Institución](https://img.shields.io/badge/UCC-Universidad%20Cat%C3%B3lica%20de%20C%C3%B3rdoba-blue)](#)

---

## 📌 Acerca de SONIA

**SONIA** es un sistema de orquestación conversacional integrado a la API de WhatsApp Business que permite a PYMEs de servicios gestionar conversaciones con clientes a través de agentes de inteligencia artificial configurables y con identidad de marca personalizada. 

El núcleo del sistema es un motor de composición modular de prompts que ensambla dinámicamente las instrucciones del agente a partir de seis componentes persistentes (objetivo, personalidad, comportamiento, tono, voz de marca y reglas) y reescribes adaptativamente el contexto en cada turno de conversación. Además, resuelve la naturaleza fragmentada de la mensajería instantánea mediante un agregador inteligente con timer adaptativo y gestiona transiciones hacia operadores humanos mediante un clasificador de transferencia que preserva el contexto completo.

---

## ✨ Características Principales

### 🧠 Módulo 1: Motor de Composición y Reescritura de Prompts
* **Composición Modular**: Configuración independiente de seis componentes (objetivo, personalidad, comportamiento, tono, voz de marca y reglas).
* **Reescritura Adaptativa**: Actualización continua del contexto conversacional por turno para inyectar objeciones, intenciones y datos detectados del cliente.
* **Pipeline RAG**: Ingesta de documentos de conocimiento del negocio (hasta 100 MB por agente) para respaldar las respuestas con búsqueda semántica.
* **Transparencia y Previsualización**: Inspección en tiempo real del prompt ensamblado y del razonamiento interno empleado por el agente.

### ⚙️ Módulo 2: Agregador Inteligente y Clasificador de Transferencia
* **Timer Adaptativo y Chunking Dinámico**: Agrupación de mensajes fragmentados en burbujas consecutivas y segmentación por intenciones.
* **Procesamiento de Audios**: Recepción, procesamiento y transcripción automática de mensajes de voz para su lectura e interpretación por el modelo de lenguaje.
* **Clasificador de Transferencia Humano-IA**: Derivación automática hacia operadores humanos ante señales de frustración, reiteración de fallas o consultas fuera de dominio.

### 💻 Módulo 3: Plataforma Operativa Centralizada
* **Bandeja Operativa (Inbox)**: Gestión de conversaciones en tiempo real mediante WebSockets, asignación de chats y control de modo del agente (Autónomo, Sugerencia, Apagado).
* **Gestión de Contactos y Etiquetas**: ABM de contactos (clientes, prospectos, proveedores) con segmentación por etiquetas.
* **Protección de Datos Sensibles**: Almacenamiento encriptado de datos personales (DNI, CUIL, CBU) en conformidad con la Ley 25.326.
* **Roles y Permisos**: Esquema de acceso diferenciado para operadores (User), supervisores (Super Agent) y administradores (Admin).
* **Panel de Métricas**: Indicadores de tasa de resolución autónoma, tiempos de respuesta y evaluación de consistencia de tono mediante *LLM-as-a-judge*.

---

## 🛠️ Tecnologías y Herramientas

* **Backend**: Go / Python.
* **Frontend**: React
* **Persistencia de Datos**: PostgreSQL.
* **Almacenamiento Vectorial**: pgvector / Qdrant.
* **Modelos de Lenguaje (LLM)**: GPT-4o / Claude API.
* **Canal de Integración**: WhatsApp Business Cloud API (desarrollo sobre Twilio Sandbox).
* **Pruebas y Calidad**: Karate Framework & SonarQube.

---

## 🏗️ Arquitectura General

```text
┌─────────────────────────┐
│  Cliente (WhatsApp API) │
└────────────┬────────────┘
             │ Webhook HTTPS
             ▼
┌─────────────────────────┐
│    Gateway WhatsApp     │
└────────────┬────────────┘
             │
             ▼
┌────────────────────────────────────────────────────────┐
│     Módulo 2: Agregador, Audios y Clasificador          │
└────────────┬───────────────────────────────┬───────────┘
             │                               │
             ▼                               ▼
┌───────────────────────────┐   ┌───────────────────────────┐
│ Módulo 1: Motor Prompts   │   │ Módulo 3: Inbox Operativo │
│  & Pipeline RAG (DB)      │◄─►│   WebSockets & Métricas   │
└───────────────────────────┘   └───────────────────────────┘
```

---

## 👥 Equipo de Trabajo

**Autores**
* Agustina Perez Anziani
* Mateo Josué Velásquez Borda
* Tobías Valentín Nicolás


**Docentes Tutores**
* Ignacio Luciano Carreño
* Federico Eduardo Porrini
* Leandro Juarez

---

## 🎓 Contexto Académico
Proyecto Integrador para la carrera de Ingeniería de Sistemas en la Universidad Católica de Córdoba (Facultad de Ingeniería), Córdoba, Argentina (2026–2027).