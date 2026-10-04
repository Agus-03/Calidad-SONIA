# ADR-0072 — Qdrant en el compose: un servicio más del stack local, y otro Qdrant sólo si se lo nombra

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · Qdrant como servicio del compose base, con volumen propio, decidido por el grupo el 2026-10-04 · las reglas 3 y 4 son decisiones de construcción |
| **Fecha** | 2026-10-04 |
| **Etapa** | Construcción — Fase 12 (conocimiento) |
| **Resuelve** | Dónde corre Qdrant en desarrollo, que ningún ADR fijaba: [ADR-0010](0010-qdrant-como-base-vectorial.md) eligió Qdrant y [ADR-0071](0071-worker-de-ingesta.md) lo puso sólo en el compose del *smoke* |
| **Requisitos afectados** | F06 |
| **Enmienda** | [ADR-0071](0071-worker-de-ingesta.md), **regla 3** — *"Qdrant corre en un contenedor del compose del smoke"* deja de ser cierto: corre como servicio del compose base, que el *smoke* también levanta. **Lo que no cambia es el resto de la regla**: en el *smoke* Qdrant sigue siendo real, y Voyage y la descripción siguen doblados |
| **Relacionada con** | [ADR-0010](0010-qdrant-como-base-vectorial.md), [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md), [ADR-0064](0064-ingesta-de-documentos.md), [ADR-0071](0071-worker-de-ingesta.md) |

---

## 1. Contexto

El `worker` de [ADR-0064](0064-ingesta-de-documentos.md) escribe los vectores en Qdrant, y la búsqueda de F06 los va a leer de ahí. [ADR-0071](0071-worker-de-ingesta.md) resolvió dónde está Qdrant en el *smoke*, un contenedor de su compose, pero no dónde está en desarrollo. El compose base daba por hecho que Qdrant *"corre fuera de este compose"* y que el `.env` lo nombra.

Con eso, levantar el stack local para probar F06 exigía levantar Qdrant a mano, con la versión correcta, y apuntarle el `worker` por `host.docker.internal`. Sin ese paso el `worker` no arranca.

**Lo que no está decidido es dónde corre Qdrant en producción.** [ADR-0017](0017-decomposicion-en-despliegues-y-lenguajes.md) fija los artefactos y deja abierto el despliegue, y este ADR no lo cierra.

## 2. Decisión

| # | Regla |
|---|---|
| 1 | **Qdrant es un servicio del compose base** (`deploy/docker-compose.yml`), con la imagen fijada en `qdrant/qdrant:v1.19.1` y los datos en el volumen `qdrantdata`. Su puerto se publica sólo en el *loopback*, como el de la base |
| 2 | **El `worker` usa el Qdrant del compose salvo que el `.env` nombre otro.** El compose le pasa `${QDRANT_URL:-http://qdrant:6333}`: vacía, apunta al servicio `qdrant`; completa, al Qdrant que nombre, con su `QDRANT_API_KEY` |
| 3 | **El *smoke* usa el Qdrant del compose base** y no define uno propio. Fuerza `QDRANT_URL=http://qdrant:6333`, así que no depende de lo que diga el `.env` |
| 4 | **El `worker` espera a que Qdrant arranque, no a que esté sano.** La imagen no trae ni `curl` ni `wget` para un *healthcheck* |

## 3. Justificación

- **Sigue siendo un servicio aparte.** Lo que [ADR-0010](0010-qdrant-como-base-vectorial.md) pide es que reindexar no compita con PostgreSQL, y Qdrant sigue en su propio contenedor, con su memoria y su disco. Que lo levante el compose no lo mete adentro de otro proceso.
- **Un solo comando levanta todo lo que F06 necesita.** Con Qdrant afuera, cada integrante tiene que acordarse de levantarlo y de qué versión. Si se olvida, el `worker` no arranca y el error no dice por qué.
- **Una sola versión fijada.** Desarrollo y *smoke* corren el mismo Qdrant, declarado en un solo lugar: lo que se prueba es lo que se usa.
- **La variable deja abierta la producción.** Si Qdrant termina en un servicio administrado o en otro servidor, se completa `QDRANT_URL` y no cambia el código ni el compose.
- **Esperar al arranque y no a la salud** porque un Qdrant que todavía está levantando le cuesta al `worker` un solo ciclo fallido, y el siguiente reintenta (ADR-0071, regla 7). Un *healthcheck* exigiría una imagen propia sólo para eso.

## 4. Consecuencias

### Positivas

- Levantar el stack local deja F06 entero andando, sin pasos manuales.
- El *dashboard* de Qdrant queda en `http://127.0.0.1:6333/dashboard` mientras el stack corre.
- Los vectores sobreviven a un reinicio del stack, como las filas de la base.

### Negativas / costos asumidos

- **Desarrollo no es producción.** En local Qdrant corre sin clave y en la misma máquina; en producción probablemente tenga clave, red de por medio y otra latencia, y nada de eso se prueba en local.
- **Qdrant ocupa memoria en todo stack local**, aunque quien lo levante no esté trabajando en F06.
- **El *smoke* y el desarrollo comparten el volumen.** Lo que indexa uno lo ve el otro, y un `down -v` del stack borra los vectores junto con las filas.
- **Un cambio de modelo de embeddings deja vectores viejos en el volumen.** Pasar del doble de embeddings a Voyage, o al revés, sin reindexar mezcla en una colección vectores que no son comparables, y Qdrant no lo detecta si la dimensión es la misma.
- **Sin *healthcheck*, el primer ciclo del `worker` puede fallar** mientras Qdrant arranca, y queda un error en el log que no es un problema.
- **El *smoke* ya no tiene un Qdrant propio.** Correrlo levanta el del compose base, con su volumen, y no un contenedor descartable.

### Lo que queda abierto

| Qué | Estado |
|---|---|
| **Dónde corre Qdrant en producción** | Se decide con el despliegue de OE7. La regla 2 lo resuelve sin tocar código |

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Qdrant aparte, levantado a mano con `docker run`** | Cada integrante tiene que levantarlo con la versión correcta y apuntarle el `worker` por `host.docker.internal`. Si se olvida, el `worker` no arranca |
| **Qdrant sólo en el compose del *smoke*** | El stack de desarrollo no puede probar F06, y el `worker` no arranca sin un Qdrant |
| **Un servicio administrado desde el desarrollo** | Suma una clave, red y una cuenta para trabajar en local, y saca los documentos de prueba de la máquina |
| **Una imagen propia con *healthcheck*** | Una imagen más que mantener para ahorrar un ciclo fallido del `worker` |
