# Ejecución reproducible de calidad

El comando de aceptación es `make quality` desde la raíz. En Windows, después de instalar las dependencias, ejecutar:

```powershell
src/brain/.venv/Scripts/python.exe scripts/quality.py
```

Preparación: Python 3.14, Poetry 2.4.3, Go según `src/core/go.mod`, Docker Desktop activo y Node 22. Bun 1.2.17 es opcional; sin él, el corredor lo ejecuta con npx. Instalar Brain con `poetry -C src/brain install`, configurando `POETRY_VIRTUALENVS_IN_PROJECT=true`. Instalar Ruff en ese entorno con `src/brain/.venv/Scripts/python.exe -m pip install -r scripts/requirements-quality.txt` (en Linux, usar `.venv/bin/python`).

El corredor compila y levanta `deploy/docker-compose.quality.yml`, aplica las migraciones mediante Core, carga la semilla y comprueba formato Go, dependencias, `go vet`, compilación, contratos, Ruff, integración Go/Python, lint y build del frontend, smoke, estrés, esfuerzo sostenido, duplicación y cobertura. La configuración es independiente de `.env` y de los servicios habituales del proyecto. Las credenciales son fixtures exclusivas de test; los proveedores de IA, el canal y el almacenamiento cloud se controlan con dobles. PostgreSQL, Qdrant, Core, Brain y Worker se ejecutan realmente.

Las pruebas de integración y smoke deben tener cero omisiones. `-count=1` evita aprobar contra resultados Go almacenados. En GitHub Actions se agrega `-race`. El frontend todavía no dispone de suite funcional automatizada: lint y build no la reemplazan.

Los puertos reservados son 55432, 56333, 54443, 58080 y 58000, ligados a loopback. No se usan volúmenes de desarrollo. El corredor elimina solamente el proyecto `sonia-quality` al terminar. `--keep-stack` permite inspeccionarlo; para limpiarlo, ejecutar `docker compose -f deploy/docker-compose.quality.yml down --volumes`. `--no-build` permite repetir contra las imágenes recién compiladas; después de modificar código de aplicación, reconstruirlas.

## Criterios fijados antes de la ejecución

| Control | Aceptación | Evidencia automática |
|---|---|---|
| Construcción y análisis estático | Ningún error de build, go vet, Ruff E9/F63/F7/F82 ni oxlint | `reports/*build.txt`, `*static.txt`, `go-vet.txt`, `frontend-lint.txt` |
| Integración y smoke | Cero fallos y cero omisiones; servicios disponibles y semilla aplicada | `go-tests.txt`, `python-tests.xml`, `smoke.txt` |
| Cobertura existente | Go y Brain >=80 %, por separado | `quality-gate.json`, `python-coverage.json`, `src/core/coverage.out` |
| Código nuevo | >=80 % de líneas ejecutables modificadas por servicio, según diff contra la base | `quality-gate.json` |
| Duplicación | <=5 % en producción Go/Python; exclusión explícita de tests y generados | `duplication/jscpd-report.json` |
| Estrés de salud | Concurrencia 5/10/20, 5 segundos por escalón, p95 <=1 s y cero errores | `load.json` |
| Estrés conversacional | Concurrencia 1/3/5, 5 segundos por escalón, p95 <=5 s y cero errores | `load.json` |
| Esfuerzo sostenido conversacional | 3 clientes durante 60 segundos; p95 <=5 s, cero errores y al menos un turno completado | `load.json` |

Los umbrales de carga son criterios de esta campaña controlada; no son SLA de producción. El tiempo conversacional termina cuando se observan lote procesado, generación `auto_sent` y mensaje `sent`; un HTTP 200 del webhook no cuenta como turno exitoso. Cada solicitud usa un cliente y un identificador únicos. No representa capacidad con proveedores reales ni demuestra disponibilidad prolongada.

`quality_gate.py` mide sentencias Go ponderadas por bloque y líneas/ramos Python mediante coverage.py. Para código modificado, cruza las líneas del diff con los rangos ejecutables de Go y las líneas ejecutables Python; ignora cambios de espacios, tests, código generado y `main.go` según ADR-0028. No es una medición de ramas Go ni una certificación de ausencia de errores. Si no hay líneas de aplicación modificadas, el indicador de código nuevo es no aplicable, sin inventar un 100 %. En PR se compara con el SHA base; en push, con el commit previo. Localmente `--base REF` permite fijar la misma comparación. El reporte identifica la base utilizada.

Los errores detectados por los analizadores y los incumplimientos de umbrales bloquean CI. Estos analizadores no clasifican exhaustivamente todos los bugs críticos posibles; sigue siendo necesaria la revisión del código y de seguridad. El informe no atribuye resultados a SonarQube.

## Evidencia y contingencia

El resultado de cada paso, duración y código de salida se guarda en `reports/execution.json`. Los reportes y logs de los servicios se publican como artefactos de GitHub Actions incluso si falla la ejecución. Una ejecución local aprobada no demuestra una ejecución remota de Actions: se deben adjuntar los enlaces de una ejecución verde y otra roja cuando se publique el cambio.

Si Docker, PostgreSQL o Qdrant no están disponibles, la aceptación falla. Las suites unitarias se pueden ejecutar por separado para diagnosticar, registrando expresamente sus omisiones. Si falla un proveedor real, el entorno de aceptación usa el doble autorizado exclusivamente en `test`; esto permite verificar contratos y orquestación, pero requiere una campaña separada antes de afirmar rendimiento o calidad de respuestas reales.

## Herramientas incorporadas en cinco pasos

1. **Requisitos:** detectar errores estáticos Python sin ejecutar proveedores; medir duplicación Go/Python y rechazar resultados por encima de 5 %; generar reportes reproducibles en CI.
2. **Alternativas:** Ruff, Flake8 y Pylint para Python; jscpd y SonarQube para duplicación. Es una comparación de adecuación al proyecto, no un benchmark de rendimiento de las alternativas.
3. **Prueba de concepto:** Ruff 0.17.2 con reglas E9/F63/F7/F82 y jscpd 5.4.2 sobre producción, excluyendo dobles de prueba y generados mediante `.jscpd.json`. Conservar comandos, salidas y JSON.
4. **Decisión:** Ruff ofrece CLI sin servidor y reglas acotadas; Flake8 requiere configuración/plugins para extender comprobaciones; Pylint aporta controles más amplios que requieren adopción gradual. jscpd cubre ambos lenguajes mediante CLI y JSON sin servidor; SonarQube agrega un servicio y configuración externa. Se eligen Ruff y jscpd para el primer gate reproducible. No se atribuyen puntuaciones ni resultados a herramientas no ejecutadas.
5. **Despliegue incremental:** medición local, regresiones negativas del gate y posterior ejecución del mismo comando en CI. Ampliar reglas y seguridad por etapas; no reducir el 80 % ni aumentar el 5 % para obtener un verde.
