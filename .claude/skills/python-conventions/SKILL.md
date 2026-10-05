---
name: python-conventions
description: 'Convenciones del Python del cerebro (`src/brain/`): async, Pydantic v2, manejo de errores, logging y el puerto de proveedores de LLM. Usar antes de escribir o revisar cualquier archivo `.py` de este repositorio.'
---

# Python — convenciones de `src/brain/`

## 1. Alcance

Todo el Python de `src/brain/`. `src/core/` es Go y no se toca desde acá. La estructura de carpetas y sus reglas están en `docs/design/08-structure-rules.md`; las convenciones de modelado, en `docs/design/00-convenciones.md`.

El cerebro hace **DML, nunca DDL**: `src/core/migrations/` es el único dueño del esquema (regla *«Sólo `core/migrations/` toca el esquema»*). Y no escribe `conversations` ni `messages`: devuelve la respuesta y el núcleo la persiste y la envía.

`src/test/` —el instrumental de evaluación offline, que no se despliega— queda **fuera** de este skill: no es un servicio, así que las reglas de async, pool y logging de acá no le aplican. Cuando exista, sus reglas se deciden entonces.

## 2. Async y bloqueo del event loop

- **Async donde hay I/O; sync en la lógica pura.** Los repositorios —`prompts/repository.py`, `pipeline/repository/`— son `async def` y reciben `AsyncConnection`; `assemble()` de `prompts/assembler.py` es sync a propósito, y por eso se prueba sin event loop (`tests/test_prompts.py`). Un `async def` sin I/O adentro agrega un `await` en cada llamada y no gana nada.
- Nunca I/O sincrónico **dentro de** un `async def`: ni un driver sync, ni un `open()` bloqueante, ni `time.sleep`. Bloquea el event loop para **todos** los turnos, no sólo para el que llama.
- La excepción declarada: leer un archivo de `rules/` con `path.read_text()` es bloqueante y está bien, porque ocurre **en la construcción** y queda cacheado con `lru_cache`, no una vez por turno. Si algo hay que leerlo por request, la regla anterior vuelve a aplicar.
- **Toda llamada externa lleva timeout, y el valor sale de `Settings`** (`llm_timeout_seconds`), nunca de un literal en la llamada.
- Cuando dos pasos son independientes, `asyncio.gather()`. Es una guía, no idioma establecido: antes de citarla como precedente, verificá con `Grep` que el cerebro ya la use.

## 3. Pydantic v2, y dónde va Pydantic

- **Pydantic es el contrato HTTP.** Los modelos viven en `api/schemas/` y son lo que se publica en `contracts/brain.openapi.json`. Los puertos internos usan dataclasses `frozen` —`LLMMessage`, `Completion`—, no modelos Pydantic. Convertir un puerto a Pydantic es deshacer una decisión, no una mejora.
- `model_validator` y `field_validator`, nunca el `validator` de v1.
- `.model_dump()` y no `.dict()`; `.model_validate()` y no `.parse_obj()`.
- `model_config = ConfigDict(...)` y no `class Config` — y en una clase `BaseSettings`, `SettingsConfigDict`.
- Lo que entra por HTTP se valida con su esquema antes de usarse. Si el esquema cambia, el error tiene que ser claro y rastreable.

## 4. Manejo de errores

- Cada paso de un flujo tiene su propio `try/except`, acotado a lo que ese paso puede fallar.
- Excepciones propias, nombradas por la condición y declaradas en el módulo que las levanta: `RulesError` en `prompts/assembler.py`, `PricingError` y `UnpricedModel` en `pipeline/generation.py`, `TurnNotFound` en `pipeline/repository/turn.py`, `ConfigError` en `infra/config.py`. No hay un `exceptions.py` central.
- Nunca `raise Exception` pelado. Una captura amplia se admite **sólo en un límite** —con un SDK de terceros, como en `adapters/llm/base.py`— y va marcada con el motivo: `# noqa: BLE001 - the boundary with a third-party SDK, whose exceptions are its own business`.
- Una jerarquía de excepciones existe para poder **decidir**: `LLMError` → `LLMTimeout` / `LLMRefusal` / `LLMUnavailable` está separada porque reintentar una negativa del proveedor quema el presupuesto del turno.
- El contexto del error dice **qué paso falló, con qué entrada, y cuál fue el error original**.

## 5. Logging

- `logging.getLogger("sonia.<módulo>")` sobre lo que configura `infra/logging.py`. Nunca `print()` ni un `basicConfig` propio.
- Los campos estructurados van **siempre** dentro de `extra={"context": {...}}`. El formateador sube al JSON sólo `record.context`, y sólo si es un dict: un `extra={"agent_id": x}` no falla, no avisa, y **no aparece en el log**.
- Nunca loguear datos personales ni el mensaje completo de la clienta sin la redacción de ADR-0006.

## 6. Proveedores de LLM

El puerto ya existe y fija la forma de lo que venga: `adapters/llm/base.py` declara `LLMClient` como `Protocol`, con las respuestas como dataclasses `frozen`, y `adapters/llm/registry.py` resuelve `ai_agents.llm_provider` a un cliente concreto con `resolve(provider, settings)`. Los proveedores disponibles son las filas de `_PROVIDERS` en ese archivo: se leen, no se suponen.

**Cuando se agregue un proveedor** es una clase nueva en `adapters/llm/<proveedor>.py` que cumple el `Protocol`, más su fila en `_PROVIDERS` con el atributo de `Settings` que guarda su clave. Nada más cambia.

Dos detalles del registro que son decisiones y no descuidos:

- `resolve()` **no levanta excepción** ante un proveedor desconocido o sin clave: devuelve un `UnavailableClient`, que levanta `LLMUnavailable` recién cuando alguien lo usa, así que toda falla del turno llega a un único `except LLMError`. Un `raise` ahí adelanta el error al arranque y rompe el resto de los agentes.
- El cliente está **cacheado con `lru_cache`, uno por clave**: construir uno por turno tira el pool de conexiones del SDK.

Y la regla que sostiene todo esto: **ningún servicio importa el SDK de un proveedor.** Se habla con el puerto. Es lo que permite que OE2 compare proveedores sin tocar el resto del cerebro.
