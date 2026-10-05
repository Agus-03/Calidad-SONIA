# ADR-0024 — Unidad de "consulta" y alcance de "sin escalada" (OE6)

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · §2, §3 y §4 enmendadas el 2026-09-20 por [ADR-0041](0041-temporizador-del-turno-por-poller.md) — el alcance se acota a las conversaciones en modo autónomo · **§2, §3, §4 y §5 enmendadas el 2026-09-26 por [ADR-0046](0046-modo-sugerencia.md)**: el modo `suggestion` vuelve a abrir bloque, y el alcance autónomo se sostiene con un filtro sobre el `detail_json` del `triage` en vez de con la ausencia del bloque. La alternativa descartada el 2026-09-20 queda adoptada con otro argumento: el modo la necesita para funcionar · **§2 enmendada el 2026-10-04 por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md)**: un bloque que el `grounding` cortó por `not_grounded` también es una escalada, haya transferencia o no, y los agentes sin base de conocimiento quedan fuera del alcance |
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
- **"Sin escalada" = sin transferencia de tipo `out_of_domain`.** Las otras tres señales de F12 no cuentan contra OE6. *(Enmendada por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 1: un bloque que el `grounding` cortó por `not_grounded` también es una escalada, aunque la política `out_of_domain` esté apagada y no haya transferencia.)*
- **Los turnos posteriores a una escalada quedan fuera del denominador**: desde ahí la conversación no la atiende el bot.
- **El alcance son las conversaciones en modo autónomo.** Sólo se abre bloque cuando el AIAgent responde solo —regla 8 de [ADR-0041](0041-temporizador-del-turno-por-poller.md)—, así que una conversación en modo `suggestion` no tiene turnos y **no entra ni en el numerador ni en el denominador de OE6**. *(Enmienda del 2026-09-20: la Fase 7 hizo esto cierto en el código antes de que nadie lo decidiera, y acá se decide.)* *(Enmendada por [ADR-0075](0075-medicion-de-oe6-y-evaluacion-de-recuperacion.md), regla 2: quedan fuera además los agentes sin base de conocimiento, que no recuperan y darían 100 % por construcción.)*

## 3. Justificación

- **El bloque es la unidad que el sistema ya materializa.** `pipeline_steps` y `signal_evaluations` cuelgan de él, y el veredicto de `triage` que da el denominador se emite una vez por bloque. Cualquier otra unidad exige reconstruirla.
- **OE6 mide cobertura del RAG, no paciencia del cliente.** Si el bot contesta correctamente algo del dominio y el cliente escala igual por frustración, contar eso como falla de OE6 penaliza a la base de conocimiento por algo que no es suyo — y la frustración ya tiene su objetivo, que es OE4.
- **Contar los turnos posteriores a la escalada sería medir al bot en conversaciones que ya no atiende**, y como la escalada mueve la conversación entera, serían todos los turnos que resten.
- **OE6 mide lo que el bot resolvió solo.** El objetivo dice *"respondidas **sin escalada** al operador humano"*, y en modo `suggestion` (F02, F27) el operador interviene en **todas**: la respuesta la manda él. Contar esas conversaciones no mediría cobertura del RAG sino con qué frecuencia un humano acepta una sugerencia, que es otra pregunta. *(Enmienda del 2026-09-20.)*

## 4. Consecuencias

### Positivas

- OE6 queda escribible como consulta SQL contra el modelo, que es el criterio del Paso 6.
- Las tres definiciones **ya estaban presupuestas** en `DER/relaciones.md` §11, que redacta la cadena como *"contra la ausencia de transferencia por `signal_type = 'out_of_domain'`"*. Pasan de tácitas a explícitas.

### Negativas / costos asumidos

- **La métrica queda más benévola que la lectura literal del objetivo.** Una conversación que escala por frustración después de respuestas correctas no descuenta. Hay que declararlo: la redacción de OE6 no distingue tipos de escalada, y nosotros sí.
- **Un bloque con dos intenciones —una del dominio y otra fuera— cuenta como una sola consulta.** Se pierde granularidad que F11 sí detecta y persiste.
- **Excluir los turnos posteriores achica el denominador**, y eso vuelve la métrica más sensible en conversaciones cortas: una escalada temprana deja pocos turnos contados.
- **Los dos modos dejan de ser comparables, y eso no se arregla después.** No es sólo que el modo `suggestion` salga del denominador: como nunca se abre el bloque, el dato para compararlo con el autónomo **no existe**. Si alguna vez se quiere medir los dos con la misma unidad hay que empezar a abrir el bloque —cerrándolo con `processed_at` puesto, como en la pausa de la regla 8—, y todo lo anterior a ese cambio queda fuera para siempre. *(Enmienda del 2026-09-20.)*
- **El anteproyecto no hace esta distinción y el informe tiene que hacerla explícita.** OE6 está escrito sobre *"las consultas"*, sin decir en qué modo estaba el agente. Acotarlo a las autónomas es una lectura nuestra, defendible pero nuestra, y va declarada junto a la otra: la redacción de OE6 tampoco distingue tipos de escalada, y nosotros sí. *(Enmienda del 2026-09-20.)*

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Consulta = intención detectada** | Más fiel a F11, pero exige atribuir la escalada a una intención concreta y el modelo no lo soporta: `transfer_events` cuelga de la conversación, no de la intención. |
| **Consulta = generación** | Deja fuera los bloques que el pipeline bloqueó antes de generar, que son justamente los casos interesantes para OE6. |
| **Cualquier escalada cuenta contra OE6** | Mezcla cobertura del RAG con paciencia del cliente, y por lo tanto mezcla OE6 con OE4. |
| **Abrir el bloque igual en modo `suggestion`, cerrado con `processed_at` puesto** *(descartada el 2026-09-20)* | Haría los dos modos comparables con la misma unidad, y es la única salida que hay que elegir **antes** de tener datos. Se descarta porque el dato que agregaría no es el que OE6 pide: un turno en modo sugerencia lo contesta el operador, así que entraría al denominador de una métrica que mide al bot sin que el bot haya respondido. Comparar los dos modos es una pregunta legítima, pero es **otra** métrica, y el día que se quiera se define con su propio numerador. |
