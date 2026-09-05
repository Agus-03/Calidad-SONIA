# ADR-0024 — Unidad de "consulta" y alcance de "sin escalada" (OE6)

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 11 anticipado (definición de métrica) |
| **Resuelve** | La parte de **H-24** que ADR-0016 dejó abierta |
| **Requisitos afectados** | OE6 · F06, F11, F12 · ADR-0016 |

---

## 1. Contexto

OE6 mide *"que el 90 % de las **consultas dentro del dominio del negocio** sean respondidas **sin escalada** al operador humano"*.

ADR-0016 resolvió qué es *"dentro del dominio"*: el veredicto del paso `triage`, que se emite **antes** de recuperar y por lo tanto no es circular. Quedaban dos palabras sin definir, y cada una cambia el número:

- **"Consulta":** ¿un bloque de mensajes? ¿una generación? ¿una intención detectada? F11 admite varias intenciones por bloque, así que si un cliente manda tres mensajes que se agrupan en un bloque con dos intenciones, eso puede contarse como una consulta o como dos.
- **"Sin escalada":** ¿cuenta **cualquier** transferencia, o sólo las de tipo `out_of_domain`?

## 2. Decisión

- **Consulta = bloque** (`message_batches`), que es lo que `03-capa-prompts.md` ya llama turno. Aunque F11 detecte varias intenciones en un bloque, la unidad de medida de OE6 es el bloque.
- **"Sin escalada" = sin transferencia de tipo `out_of_domain`.** Las otras tres señales de F12 no cuentan contra OE6.
- **Los turnos posteriores a una escalada quedan fuera del denominador**: desde ahí la conversación no la atiende el bot.

## 3. Justificación

- **El bloque es la unidad que el sistema ya materializa.** `pipeline_steps` y `signal_evaluations` cuelgan de él, y el veredicto de `triage` que da el denominador se emite una vez por bloque. Cualquier otra unidad exige reconstruirla.
- **OE6 mide cobertura del RAG, no paciencia del cliente.** Si el bot contesta correctamente algo del dominio y el cliente escala igual por frustración, contar eso como falla de OE6 penaliza a la base de conocimiento por algo que no es suyo — y la frustración ya tiene su objetivo, que es OE4.
- **Contar los turnos posteriores a la escalada sería medir al bot en conversaciones que ya no atiende**, y como la escalada mueve la conversación entera, serían todos los turnos que resten.

## 4. Consecuencias

### Positivas

- OE6 queda escribible como consulta SQL contra el modelo, que es el criterio del Paso 6.
- Las tres definiciones **ya estaban presupuestas** en `DER/relaciones.md` §11, que redacta la cadena como *"contra la ausencia de transferencia por `signal_type = 'out_of_domain'`"*. Pasan de tácitas a explícitas.

### Negativas / costos asumidos

- **La métrica queda más benévola que la lectura literal del objetivo.** Una conversación que escala por frustración después de respuestas correctas no descuenta. Hay que declararlo: la redacción de OE6 no distingue tipos de escalada, y nosotros sí.
- **Un bloque con dos intenciones —una del dominio y otra fuera— cuenta como una sola consulta.** Se pierde granularidad que F11 sí detecta y persiste.
- **Excluir los turnos posteriores achica el denominador**, y eso vuelve la métrica más sensible en conversaciones cortas: una escalada temprana deja pocos turnos contados.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Consulta = intención detectada** | Más fiel a F11, pero exige atribuir la escalada a una intención concreta y el modelo no lo soporta: `transfer_events` cuelga de la conversación, no de la intención. |
| **Consulta = generación** | Deja fuera los bloques que el pipeline bloqueó antes de generar, que son justamente los casos interesantes para OE6. |
| **Cualquier escalada cuenta contra OE6** | Mezcla cobertura del RAG con paciencia del cliente, y por lo tanto mezcla OE6 con OE4. |
