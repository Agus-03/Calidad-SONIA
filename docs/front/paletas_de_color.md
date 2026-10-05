# Guía de Paletas de Color en UI Design & Adaptación a Dark Mode

## 1. Fundamentos de las Paletas de Color en UI/UX

En el diseño de interfaces de usuario (UI), una paleta de colores no es solo una elección estética, sino un **sistema funcional de comunicación**. A diferencia del diseño gráfico tradicional o la identidad de marca (*branding*), el color en UI tiene tareas sistémicas: guía la atención del usuario, transmite jerarquía, comunica el estado de los componentes (activo, deshabilitado, error) y garantiza la legibilidad mediante estándares de accesibilidad.

### 1.1. Diferencia entre Paleta de Marca y Paleta de Sistema UI
* **Paleta de Marca (Branding):** Define la personalidad y las emociones de la empresa en material publicitario, logos y marketing.
* **Paleta de Sistema UI (Design Tokens):** Adapta la marca a roles funcionales de software. Divide los colores en escalas tonales para fondos, superficies, texto, bordes y estados del sistema.

### 1.2. La Regla del 60-30-10 en Interfaces
Una técnica clásica adaptada al diseño digital para mantener la armonía visual:
* **60% - Color Dominante (Superficie/Canvas):** Fondos de la aplicación y áreas neutras.
* **30% - Color Secundario (Estructura/Contenido):** Tarjetas, contenedores, barras de navegación y tipografía.
* **10% - Color de Acento (Acción/CTA):** Botones principales, enlaces, indicadores de progreso y notificaciones.

### 1.3. Roles de Color Esenciales en UI
1. **Canvas / Background:** El fondo sobre el que descansan todos los elementos.
2. **Surface (Superficies):** Elevaciones físicas o virtuales (tarjetas, modales, menús flotantes).
3. **Primary / Brand:** Color representativo de la marca utilizado en elementos clave.
4. **Accent / Interactive:** Color con alto impacto visual reservado para llamados a la acción (CTA) y elementos interactivos.
5. **Text / Content:** Gradientes de contraste para texto primario, secundario y deshabilitado.
6. **Feedback / Semánticos:** Colores estándar para mensajes del sistema (*éxito*, *advertencia*, *error*, *información*).

---

## 2. Principios de Adaptación a Dark Mode

Crear un modo oscuro no consiste en "invertir" los colores mediante un filtro, sino en reconstruir la paleta para optimizar el contraste en entornos de baja iluminación.

### Reglas clave para el Modo Oscuro:
* **Evitar el negro puro (`#000000`):** El negro absoluto genera un contraste excesivamente agresivo con el texto blanco (efecto de "halación" o fatiga visual). Se prefieren grises oscuros o azulosos con tintes de la marca.
* **Jerarquía por Elevación (Luminancia):** En Light Mode, las sombras crean profundidad. En Dark Mode, las sombras son invisibles; la profundidad se logra **aclarando levemente** la superficie a medida que está más elevada frente al fondo.
* **Desaturación de Colores de Marca:** Los colores oscuros o altamente saturados del modo claro pierden legibilidad o "vibran" molestamente sobre fondos oscuros. El color primario debe ajustarse hacia un tono más claro y pastel/vibrante para mantener la legibilidad y accesibilidad (WCAG AA / AAA).

---

## 3. Análisis de la Paleta Light Mode (Existente)

Basado en las especificaciones provistas:

| Rol | Nombre | HEX | RGB | CMYK | Función en Light Mode |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Primario** | Verde Bosque Profundo | `#0E311F` | `14, 49, 31` | `71, 0, 37, 81` | Identidad del motor, estabilidad, botones primarios, cabeceras. |
| **Apoyo 1** | Menta Suave | `#DDF4ED` | `221, 244, 237` | `9, 0, 3, 4` | Fondos de tarjetas, contenedores secundarios, resaltado suave. |
| **Apoyo 2** | Turquesa | `#3FD0D5` | `63, 208, 213` | `70, 2, 0, 16` | Acento visual, alertas, estado de agentes, llamadas a la acción (CTA). |
| **Apoyo 3** | Azul Noche | `#01111D` | `1, 17, 29` | `97, 41, 0, 89` | Texto primario de alto contraste, bordes oscuros y elementos estructurales. |
| **Apoyo 4** | Blanco Nieve | `#F5FFF9` | `245, 255, 249` | `4, 0, 2, 0` | Fondo general de la aplicación (*Canvas*), tarjetas blancas. |

---

## 4. Propuesta de Paleta Dark Mode (Sistema Adaptado)

Para el modo oscuro, tomamos como base el **Azul Noche (`#01111D`)** para construir la estructura de fondos y elevaciones, mientras adaptamos el **Verde Bosque Profundo** a un tono esmeralda más claro que cumpla con los estándares de contraste.

### 4.1. Definición de la Paleta Dark Mode

```
+-----------------------------------------------------------------------+
|  CANVAS / FONDO BASE                                                  |
|  Azul Noche Profundo: #07121A | RGB: 7, 18, 26                        |
+-----------------------------------------------------------------------+
|  SUPERFICIE ELEVACIÓN 1 (Cards / Modales)                             |
|  Azul Noche Elevado: #0F1D26 | RGB: 15, 29, 38                       |
+-----------------------------------------------------------------------+
|  SUPERFICIE ELEVACIÓN 2 (Contenedores Destacados)                     |
|  Menta Oscura Muted: #122924 | RGB: 18, 41, 36                       |
+-----------------------------------------------------------------------+
|  COLOR PRIMARIO (Branding & Focus en Dark Mode)                       |
|  Verde Esmeralda Vivo: #38B27A | RGB: 56, 178, 122                     |
+-----------------------------------------------------------------------+
|  COLOR DE ACENTO / CTA (Botones e Interacciones)                      |
|  Turquesa Vibrante: #3FD0D5 | RGB: 63, 208, 213                       |
+-----------------------------------------------------------------------+
|  TEXTO PRINCIPAL                                                      |
|  Blanco Nieve: #F5FFF9 | RGB: 245, 255, 249                           |
+-----------------------------------------------------------------------+
|  TEXTO SECUNDARIO / SUBTÍTULOS                                        |
|  Menta Grisácea: #94B5A8 | RGB: 148, 181, 168                         |
+-----------------------------------------------------------------------+
```

### 4.2. Especificaciones Técnicas Dark Mode

#### 1. Canvas / Fondo Principal (Base)
* **HEX:** `#07121A`
* **RGB:** `7, 18, 26`
* **CMYK:** `73, 31, 0, 90`
* **Uso UI:** Reemplaza al Blanco Nieve (`#F5FFF9`) como fondo de pantalla. Derivado del Azul Noche original con matiz neutro para descanso visual.

#### 2. Superficie Elevada (Cards, Paneles, Modales)
* **HEX:** `#0F1D26`
* **RGB:** `15, 29, 38`
* **CMYK:** `61, 24, 0, 85`
* **Uso UI:** Superficie de tarjetas y paneles. Permite que las tarjetas destaquen suavemente sobre el fondo sin necesidad de sombras.

#### 3. Superficie Destacada / Contenedores Secundarios
* **HEX:** `#122924`
* **RGB:** `18, 41, 36`
* **CMYK:** `56, 0, 12, 84`
* **Uso UI:** Reemplaza el Menta Suave (`#DDF4ED`) del modo claro. Ideal para bloques de código, cajas de contexto, chat bubbles secundarias o alertas neutras.

#### 4. Primario Adaptado (Verde Esmeralda)
* **HEX:** `#38B27A`
* **RGB:** `56, 178, 122`
* **CMYK:** `69, 0, 31, 30`
* **Uso UI:** Evolución del Verde Bosque Profundo (`#0E311F`). Se aclara para garantizar un ratio de contraste de al menos 4.5:1 sobre los fondos oscuros. Se usa en iconos activos, estados confirmados y badges.

#### 5. Acento / CTA (Turquesa Original)
* **HEX:** `#3FD0D5`
* **RGB:** `63, 208, 213`
* **CMYK:** `70, 2, 0, 16`
* **Uso UI:** Conserva exactamente el mismo valor HEX que en Light Mode. Al ser un color cian luminoso, funciona como un excelente acento para botones, enlaces activos e indicadores de estado del agente.

#### 6. Texto Principal (Blanco Nieve)
* **HEX:** `#F5FFF9`
* **RGB:** `245, 255, 249`
* **CMYK:** `4, 0, 2, 0`
* **Uso UI:** En modo oscuro, el *Blanco Nieve* pasa de ser el fondo a ser la tipografía principal para maximizar la legibilidad sobre fondos oscuros.

#### 7. Texto Secundario / Muted
* **HEX:** `#94B5A8`
* **RGB:** `148, 181, 168`
* **CMYK:** `18, 0, 7, 29`
* **Uso UI:** Subtítulos, descripciones secundarias, timestamps y placeholders de formularios.

#### 8. Bordes y Divisores
* **HEX:** `#1D3538`
* **RGB:** `29, 53, 56`
* **CMYK:** `48, 5, 0, 78`
* **Uso UI:** Líneas divisorias, bordes de inputs y delimitadores de tarjetas.

---

## 5. Mapeo Sistemático de Design Tokens (Light vs Dark)

A continuación se presenta la matriz de equivalencias para implementar en un sistema de diseño utilizando variables CSS o Tailwind CSS.

| Token de UI | Light Mode | Dark Mode | Propósito |
| :--- | :--- | :--- | :--- |
| `--bg-app` | `#F5FFF9` | `#07121A` | Fondo general de la app |
| `--bg-surface` | `#FFFFFF` | `#0F1D26` | Tarjetas y contenedores |
| `--bg-surface-accent` | `#DDF4ED` | `#122924` | Tarjetas destacadas / Menta |
| `--border-color` | `#DDF4ED` | `#1D3538` | Divisores y bordes |
| `--text-primary` | `#01111D` | `#F5FFF9` | Títulos y cuerpo de texto principal |
| `--text-secondary` | `#0E311F` | `#94B5A8` | Subtítulos y descripciones |
| `--brand-primary` | `#0E311F` | `#38B27A` | Color distintivo de marca |
| `--interactive-accent` | `#3FD0D5` | `#3FD0D5` | Botones de acción / CTAs |

---

## 6. Ejemplo de Implementación en Variables CSS

```css
:root {
  /* Modo Claro (Light Mode) */
  --bg-app: #F5FFF9;
  --bg-surface: #FFFFFF;
  --bg-surface-accent: #DDF4ED;
  --border-color: #D6EBE4;
  --text-primary: #01111D;
  --text-secondary: #0E311F;
  --brand-primary: #0E311F;
  --interactive-accent: #3FD0D5;
}

[data-theme="dark"] {
  /* Modo Oscuro (Dark Mode) */
  --bg-app: #07121A;
  --bg-surface: #0F1D26;
  --bg-surface-accent: #122924;
  --border-color: #1D3538;
  --text-primary: #F5FFF9;
  --text-secondary: #94B5A8;
  --brand-primary: #38B27A;
  --interactive-accent: #3FD0D5;
}
```