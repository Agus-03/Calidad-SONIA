# ADR-0009 — Horario de atención del agente: fuera del alcance

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-08-06 |
| **Etapa** | Diseño — Paso 3 |
| **Requisitos afectados** | Ninguno · sección 5.3 |

---

## 1. Contexto

La justificación del proyecto se apoya explícitamente en la disponibilidad permanente. La sección 5.3 dice que los emprendedores *"no tienen disponibilidad 24hs para responder"*, y presenta a SONIA como la solución a esa limitación.

Sin embargo, es razonable que el cooperante prefiera atender personalmente en horario comercial y dejar el agente IA para el resto — el caso de uso real puede ser "cubrir la noche", no "reemplazar la atención". El anteproyecto no contempla esa configuración.

## 2. Decisión

**Fuera del alcance actual.** Se declara como trabajo futuro / oportunidad de mejora.

Se recomienda, de todos modos, **preguntárselo al cooperante durante el relevamiento** — no para implementarlo, sino para dejar escrito qué preferiría.

## 3. Justificación

- **Mantiene coherente la justificación de la sección 5.3.** Si el agente tuviera horario de atención, el argumento de la disponibilidad 24 horas —que es el que sostiene buena parte del "para qué" del proyecto— se debilitaría.
- **Ningún requisito de F01–F39 lo pide.** Incorporarlo sería agregar alcance, no completarlo.
- **Es una configuración, no una capacidad nueva.** Agregarlo más adelante no obliga a rehacer nada: es un atributo por agente y una condición en el enrutamiento.

## 4. Consecuencias

### Positivas

- La justificación del proyecto queda intacta y sin contradicción interna.
- Menos superficie que construir, probar y documentar en la Fase 3.

### Negativas / costos asumidos

- **Si el cooperante lo pide durante la validación (Fase 4), hay que decir que no** o salirse del alcance en el peor momento del cronograma. Preguntarlo antes reduce ese riesgo a una conversación.
- El sistema atiende siempre, incluso en situaciones donde el cooperante preferiría responder personalmente.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Incluirlo como atributo configurable por agente desde el inicio** | Es barato agregarlo después y hoy no hay ningún requisito que lo sostenga. Construir capacidad sin requisito es alcance no justificado. |
| **Incluirlo y ajustar la justificación de 5.3** | Debilitaría el argumento central del "para qué" del proyecto a cambio de una funcionalidad que nadie pidió todavía. |
