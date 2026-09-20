# ADR-0027 — El núcleo opera en una única instancia

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · §1 y §4 enmendadas por [ADR-0041](0041-temporizador-del-turno-por-poller.md): el temporizador de F10 deja de vivir en la memoria del proceso y pasa a `message_batches.expires_at`, cerrado por un poller. Con eso se cierra el pendiente que §4 dejó abierto —qué hacer al arrancar con los bloques cuyo temporizador se perdió— y el temporizador deja de ser un motivo para la instancia única; el fan-out de WebSockets de F21 sí lo sigue siendo |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 7 (nivel físico) |
| **Resuelve** | Pendiente abierto por ADR-0017: el núcleo es de instancia única y no está declarado |
| **Requisitos afectados** | F10, F21 · sección 7.3 |

---

## 1. Contexto

El núcleo guarda **dos cosas en la memoria del proceso**:

- **El temporizador de F10:** uno vivo por conversación, reiniciado con cada mensaje entrante, que cierra el bloque tras el silencio.
- **Las conexiones WebSocket de F21:** los operadores conectados **a ese proceso**.

Con dos instancias las dos se rompen, y en silencio: dos procesos podrían mantener temporizador para la misma conversación —sin dueño definido, con doble procesamiento posible—, y un mensaje que llega a la instancia A no alcanza al operador conectado a la B.

Nada en el sistema declara esta restricción hoy, así que la arquitectura **se lee como si escalara horizontalmente**.

## 2. Decisión

**El núcleo se despliega en una única instancia**, y eso se declara como **limitación explícita** en el documento de arquitectura. No se construye nada para soportar varias.

Se deja escrita la salida, para cuando haga falta y **sin tocar el alcance de 7.3**:

- **Fan-out:** `LISTEN`/`NOTIFY` de PostgreSQL. No es MQTT —que 7.3 declara fuera de alcance— y no agrega un componente nuevo.
- **Temporizador:** *leases* en la base, o ruteo pegajoso por conversación.

## 3. Justificación

- **Ningún requisito de F01–F39 pide alta disponibilidad ni escalado horizontal**, y el volumen del cooperante no lo justifica. Construir capacidad sin requisito es alcance no justificado — el mismo argumento con el que ADR-0009 dejó el horario de atención afuera.
- **Lo caro no es la limitación: es que no esté escrita.** Una arquitectura que no la declara se lee como si escalara, y la pregunta aparece en la defensa sin respuesta preparada.
- **El costo de levantarla después es acotado y conocido**, y las dos salidas ya están identificadas. Declararla no cierra la puerta.

## 4. Consecuencias

### Positivas

- El temporizador de F10 y el fan-out de F21 se implementan de la forma más simple posible, sin coordinación distribuida.
- La limitación queda documentada junto con su salida, así que es una decisión y no un descuido.

### Negativas / costos asumidos

- **No hay alta disponibilidad.** Reiniciar el núcleo corta todos los WebSockets conectados.
- **Un reinicio pierde los temporizadores en vuelo.** Los bloques abiertos quedan sin cerrar, y **qué se hace con ellos al arrancar está sin definir**: ¿se cierran los que tengan `opened_at` vencido? ¿se descartan? **Queda abierto, y es lo primero que hay que resolver de este ADR** — es la única consecuencia que produce datos inconsistentes, no sólo interrupción.
- **Cada despliegue es una interrupción visible** para los operadores conectados.
- **OE7 mide producción real con este límite encima.** Si el cooperante tuviera un pico de tráfico, no hay salida por escalado horizontal dentro del alcance actual.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Soportar varias instancias desde el inicio** | Agrega coordinación distribuida —*leases*, fan-out por la base— a un proyecto que no la pide en ningún requisito. |
| **No declararlo** | Deja que la arquitectura se lea como si escalara. El costo aparece en la defensa, cuando ya no hay tiempo de responder bien. |
