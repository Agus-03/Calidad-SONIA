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

Si no se quiere depender de `make`, los dos targets están en el [`Makefile`](../../Makefile) y son una línea cada uno.

> **`contracts-check` cubre dos cosas distintas con un solo comando.** Las 22 enumeraciones, que viven tres veces —`enums.yaml`, Go y Python—, y el contrato del único salto sincrónico entre los dos desplegables: `contracts/brain.openapi.json` se vuelca de la propia app de FastAPI, así que un campo agregado en un esquema de Pydantic **cambia el archivo y rompe el chequeo si nadie lo regeneró**. Lo que todavía no cubre es que los structs de Go en `brainclient/contract.go` cumplan ese contrato: eso sigue siendo una transcripción a mano.

## Lo que el runner tiene que proveer

**Un PostgreSQL 17 y la variable `SONIA_TEST_DATABASE_URL` apuntándole.** Es el punto más importante de todo este archivo.

Los tests de integración —cuatro del corredor de migraciones, dos del pool, uno del arranque— **se saltean solos cuando esa variable no está**. Un CI sin base de datos no falla: pasa en verde habiendo probado la mitad. Y lo que dejaría de verificarse es justamente lo más caro de descubrir tarde: que el esquema de 37 tablas aplica, que migrar dos veces no rompe nada y que el proceso levanta contra una base real.

Con la base disponible la cobertura del núcleo sube del 46 % al 80 %, así que **el corte del 80 % es inalcanzable sin ella**. No es opcional.

El resto de las variables de entorno no hacen falta: los tests que necesitan configuración se la arman ellos.

## Qué queda fuera del CI, a propósito

- **Levantar el `docker-compose`.** Los tests hablan con una base; no necesitan el stack entero.
- **pgAdmin** ([`deploy/docker-compose.dev.yml`](../../deploy/docker-compose.dev.yml)). Es herramienta de desarrollo y está en un compose aparte para que nada la despliegue por accidente.
- **Los seeds.** Todavía no existen.

## Qué se excluye de la medición, y por qué

| Excluido | Servicio | Motivo |
|---|---|---|
| `enums_gen.go` · `infra/enums.py` | ambos | Código generado desde `contracts/enums.yaml`. En Python se ejecuta entero al importarse y regalaría cobertura; en Go son 393 líneas que nadie escribió a mano |
| `src/core/main.go` | núcleo | Sólo `main()`, la señal y el código de salida. Lo que se puede probar vive en `internal/app`, que existe precisamente para eso |
| `src/brain/tests/` | cerebro | Los tests no son lo que se está midiendo |
