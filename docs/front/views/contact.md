# Documento de Diseño: Vista de Contacto (Panel Lateral)

## Descripción General
Esta vista consiste en un panel lateral derecho que se despliega o mantiene fijo junto a la ventana de "Chat Activo". Su objetivo es proporcionar al operador del CRM todo el contexto posible sobre el cliente con el que está interactuando, sin necesidad de cambiar de pantalla.

Al ser una Single Page App (SPA), este componente debe actualizarse dinámicamente cuando el operador selecciona un chat diferente en la vista de *Inbox*.

---

## Layout y Distribución de Elementos

El panel se divide verticalmente en las siguientes sub-vistas o secciones:

### 1. Cabecera de Perfil (Profile Header)
Ubicada en la parte superior del panel. Sirve para identificar rápidamente al contacto.
*   **Avatar/Foto de Perfil:** Imagen circular. Si no tiene foto, mostrar iniciales.
*   **Nombre del Contacto:** Texto destacado (H3 o H4).
*   **Número de WhatsApp:** Subtítulo con el número de teléfono con formato internacional.
*   **Estado:** Un pequeño indicador visual (punto verde si está en línea, gris si no). *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

### 2. Acciones Rápidas (Quick Actions)
Botones icónicos debajo de la cabecera para acciones inmediatas sobre el contacto.
*   **Botón "Editar Contacto":** Abre un modal para modificar nombre u otros datos.
*   **Botón "Silenciar/Bloquear":** Para manejo de spam.
*   **Botón "Resolver/Cerrar Chat":** Acción principal del ticket.
*   *(Nota general para acciones: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

### 3. Información Estándar (Standard Information)
Una lista de campos predefinidos que todo contacto puede tener.
*   **Correo Electrónico:** Icono de sobre + texto.
*   **Ubicación/País:** Icono de pin + texto (puede deducirse del prefijo del teléfono).
*   **Empresa:** Nombre de la compañía a la que pertenece.
*   **Redes Sociales:** Links a LinkedIn o Instagram si los hay.
*   *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

### 4. Sistema de Etiquetas (Tags)
Una sub-vista crucial para la organización del CRM.
*   **Contenedor de Etiquetas:** Muestra "píldoras" (badges) de colores con las etiquetas actuales del contacto (ej. "Cliente VIP", "Soporte Técnico", "Lead").
*   **Botón "+ Añadir Etiqueta":** Un input con autocompletado para asignar nuevas etiquetas.
*   *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

### 5. Atributos Personalizados (Custom Attributes)
Sección desplegable (Acordeón) para datos específicos del negocio (ej. "Número de cliente", "Plan contratado", "Fecha de vencimiento").
*   Pares de Clave-Valor dinámicos.
*   *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

### 6. Pestañas de Contexto Inferior (Bottom Tabs)
Para no saturar la vista, la mitad inferior del panel puede dividirse en pestañas (Tabs):

*   **Tab A: Notas Privadas (Private Notes)**
    *   Un área de texto donde los operadores pueden dejarse mensajes internos sobre este contacto que el cliente no verá.
    *   Historial de notas con fecha y nombre del operador que la dejó.
    *   *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

*   **Tab B: Conversaciones Previas (Previous Conversations)**
    *   Lista compacta de tickets o chats anteriores cerrados con este mismo número.
    *   Al hacer clic en uno, debería permitir ver un modal con el historial de esa charla pasada.
    *   *(Nota: solo realizar si se puede hacer con el backend, sino dejar un mensaje de falta de implementación/integración).*

---

## Comportamiento UI/UX (Frontend)
*   **Responsive:** En pantallas pequeñas (móviles), este panel derecho debería ocultarse y ser accesible solo mediante un botón de "Ver info de contacto" en la cabecera del chat, abriéndose como un *Offcanvas* o panel superpuesto.
*   **Estados de Carga (Loading Skeletons):** Mientras el frontend consulta al backend por los atributos y notas del contacto, se deben mostrar *skeletons* (cajas grises parpadeantes) en lugar de dejar la pantalla en blanco.
*   **Manejo de Errores:** En los espacios donde aplique la restricción de backend, mostrar un pequeño banner o texto en gris itálica: *"Funcionalidad no integrada con el backend"* para no romper el diseño si la API aún no soporta ese endpoint.
```eof