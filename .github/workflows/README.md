# Qué tiene que correr el CI

Este archivo existe para que quien escriba el workflow no tenga que deducir los comandos leyendo el repositorio. **No describe cómo se configura GitHub Actions** —runners, caché, secretos, matrices— : eso es decisión de quien lo arme.

La regla que gobierna todo lo de abajo es [ADR-0028](../../docs/design/02-decisiones/0028-cobertura-de-pruebas-por-servicio.md): la cobertura se mide **≥ 80 % por servicio y nunca agregada**. Por eso son dos trabajos separados y no uno.

---

## Los comandos

Los umbrales **no van en el workflow**. Viven en el repositorio —`Makefile` para Go, `pyproject.toml` para Python— así que estos comandos ya fallan solos cuando la cobertura baja.

| Trabajo | Directorio | Comando |
|---|---|---|
| Formato de Go | `src/core` | `gofmt -l .` — **falla si imprime cualquier cosa**; no devuelve código de error por su cuenta |
| Núcleo | `src/core` | `go vet ./...` y después `go build ./...` |
| Núcleo | raíz | `make test-core` — corre los tests, filtra lo generado y corta abajo del 80 % |
| Cerebro | `src/brain` | `poetry install` y después `poetry run pytest` — el corte está en `addopts` |
| Contrato | raíz | `make contracts-check` — falla si las enumeraciones generadas o `contracts/brain.openapi.json` quedaron viejos |
| Punta a punta | raíz | `make smoke` — levanta el stack con los dos extremos doblados, lo siembra y recorre el camino entero |

Si no se quiere depender de `make`, los targets están en el [`Makefile`](../../Makefile) y son una línea cada uno, salvo `smoke`, que son tres.

> **`contracts-check` cubre dos cosas distintas con un solo comando.** Las 22 enumeraciones, que viven tres veces —`enums.yaml`, Go y Python—, y el contrato del único salto sincrónico entre los dos desplegables: `contracts/brain.openapi.json` se vuelca de la propia app de FastAPI, así que un campo agregado en un esquema de Pydantic **cambia el archivo y rompe el chequeo si nadie lo regeneró**. Lo que todavía no cubre es que los structs de Go en `brainclient/contract.go` cumplan ese contrato: eso sigue siendo una transcripción a mano.

> **`make smoke` es lo único que necesita el stack entero levantado.** Todo lo demás habla con una base de datos; esto
> habla con los tres servicios, porque es lo único que puede decir que están *conectados*: cada pieza del turno tiene sus
> tests unitarios y ninguno se entera si el núcleo llama a un endpoint que el cerebro no publica. Corre con los dos dobles
> —`WHATSAPP_USE_DOUBLE` y `LLM_USE_DOUBLE`, que [`docker-compose.smoke.yml`](../../deploy/docker-compose.smoke.yml)
> enciende—, así que **no hace falta ninguna credencial**: ni clave de Cohere ni número de WhatsApp.

> **Está escrito en Go y no en Karate, que es lo que nombra la sección 8.4 del anteproyecto.** El motivo es que la mitad de
> lo que afirma son filas de PostgreSQL —que el lote quedó procesado, que la generación quedó enlazada al mensaje— y eso es
> justo lo que Karate hace incómodo, mientras que la cadena de Go ya está en el CI y Java y Maven no. El costo del desvío
> es real y hay que decirlo: **Karate sigue sin usarse en el repositorio**, y `src/test/karate/` sigue siendo una carpeta
> reservada. El plan es que entre cuando exista la API de operador, que es donde 8.4 lo pide de verdad: muchos endpoints
> HTTP y ninguna afirmación sobre la base.

## Lo que el runner tiene que proveer

**Un PostgreSQL 17 y la variable `SONIA_TEST_DATABASE_URL` apuntándole.** Es el punto más importante de todo este archivo.

Los tests de integración —cuatro del corredor de migraciones, dos del pool, uno del arranque— **se saltean solos cuando esa variable no está**. Un CI sin base de datos no falla: pasa en verde habiendo probado la mitad. Y lo que dejaría de verificarse es justamente lo más caro de descubrir tarde: que el esquema de 37 tablas aplica, que migrar dos veces no rompe nada y que el proceso levanta contra una base real.

Con la base disponible la cobertura del núcleo sube del 46 % al 80 %, así que **el corte del 80 % es inalcanzable sin ella**. No es opcional.

El resto de las variables de entorno no hacen falta: los tests que necesitan configuración se la arman ellos.

**Para `make smoke`, además: Docker y un `.env`.** El `docker-compose` lo exige por `env_file` y por las tres variables de PostgreSQL, así que un runner sin `.env` no puede levantar el stack —alcanza con copiar `.env.example`, porque los dos dobles hacen que ningún secreto se use de verdad—. El target exporta las tres variables que el caso lee: `SONIA_TEST_DATABASE_URL`, `SONIA_SMOKE_CORE_URL` y `SONIA_SMOKE_BRAIN_URL`. **Sin las tres, el caso se saltea** igual que los de integración, que es lo que impide que un CI sin Docker se ponga verde sin haber probado nada.

## Qué queda fuera del CI, a propósito

- **Levantar el `docker-compose` para los tests unitarios y de integración.** Ésos hablan con una base y no necesitan el stack entero; el único que sí lo necesita es `make smoke`, que lo levanta él mismo.
- **pgAdmin** ([`deploy/docker-compose.dev.yml`](../../deploy/docker-compose.dev.yml)). Es herramienta de desarrollo y está en un compose aparte para que nada la despliegue por accidente.
- **Forzar el camino de error del turno.** Un turno que falla deja `processed_at` en NULL con su `error_message`, y eso lo cubren los tests del orquestador. Reproducirlo de punta a punta pide romper un servicio a propósito, que es un interruptor que hoy no existe y que no se agrega para un test.

## Qué se excluye de la medición, y por qué

| Excluido | Servicio | Motivo |
|---|---|---|
| `enums_gen.go` · `infra/enums.py` | ambos | Código generado desde `contracts/enums.yaml`. En Python se ejecuta entero al importarse y regalaría cobertura; en Go son 393 líneas que nadie escribió a mano |
| `src/core/main.go` | núcleo | Sólo `main()`, la señal y el código de salida. Lo que se puede probar vive en `internal/app`, que existe precisamente para eso |
| `src/brain/tests/` | cerebro | Los tests no son lo que se está midiendo |
