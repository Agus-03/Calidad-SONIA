# ADR-0028 — La cobertura de pruebas del ≥ 80 % se exige por servicio

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 7 (nivel físico) |
| **Resuelve** | Pendiente abierto por ADR-0017: cómo se agregan dos reportes de cobertura |
| **Requisitos afectados** | OE5 |

---

## 1. Contexto

OE5 exige *"cobertura de pruebas unitarias ≥ 80 %"*. Con dos runtimes —Go en el núcleo, Python en el cerebro— hay **dos cadenas de herramientas y dos reportes**, y OE5 nombra un número, no dos.

Falta definir si el umbral se exige **por servicio** o **sobre el total agregado**. No dan el mismo resultado, y la asimetría es grande: el núcleo concentra **26 de los 39 requisitos**.

## 2. Decisión

**El ≥ 80 % se exige por servicio.** Cada uno reporta su cobertura y **ambos deben superar el umbral**; no se agregan en un número único.

El CI falla por servicio, no por promedio.

## 3. Justificación

- **Sobre el total, el servicio grande tapa al chico.** Con el núcleo al 90 % y el cerebro al 40 %, el agregado supera el 80 % y **la parte que contiene el aporte original de la tesina queda sin probar**. El número diría lo contrario de lo que pasa.
- **Evita inventar un procedimiento de agregación** entre dos herramientas distintas para producir un número que además engaña.
- Es el criterio más exigente, y el único que se sostiene solo al leer OE5 sin hacer trampa.

## 4. Consecuencias

### Positivas

- Ningún servicio se esconde detrás del otro.
- Cada cadena de herramientas reporta lo suyo, sin conversión ni normalización entre formatos.
- El CI puede fallar señalando **cuál** de los dos bajó del umbral.

### Negativas / costos asumidos

- **Es más exigente que el agregado.** El cerebro es el servicio con la lógica más difícil de testear —llamadas a un LLM externo, no deterministas— y va a exigir doblado de dependencias **desde el primer día**, no al final. Es trabajo que hay que planificar, no una consecuencia de ejecución.
- **OE5 dice un número y nosotros reportamos dos.** Hay que explicar en el informe que se interpretó como umbral por servicio y por qué — es una interpretación del objetivo, no una lectura literal.
- **El frontend queda fuera de esta cuenta.** Falta decidir si entra en OE5 y con qué umbral. **Queda abierto**, y depende de la decisión de tecnología del frontend, que sigue pendiente.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Umbral sobre el total agregado** | Permite que el núcleo tape al cerebro. Cumpliría OE5 con el aporte original sin probar. |
| **Umbral sólo sobre el núcleo** | Deja sin cubrir exactamente la parte que la tesina aporta. |
| **Promedio ponderado por cantidad de requisitos** | Es el agregado con un disfraz: el servicio con 26 de 39 requisitos sigue dominando el número. |
