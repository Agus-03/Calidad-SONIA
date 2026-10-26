# Revalidación de calidad de SONIA - 26/10/2026

**Resultado:** aceptación local completa aprobada. El cumplimiento global del TP sigue siendo parcial hasta incorporar evidencia remota de GitHub Actions y el aporte individual trazable. **Nivel declarado: 1, inicial**, conservado del informe previo (páginas 4 y 21).

Se ejecutó desde la raíz `src/brain/.venv/Scripts/python.exe scripts/quality.py`, incluyendo construcción, despliegue nuevo, migraciones, semilla, suites y limpieza del entorno. Todos los pasos de `execution.json` devolvieron 0. Las imágenes pertenecen al proyecto aislado `sonia-quality`; no se modificaron bases existentes.

## Contraste con el resumen entregado

| Indicador | Resumen previo | Revalidación | Lectura |
|---|---|---|---|
| Go | Suite aprobada | 1218 casos/subcasos internos aprobados, 0 omitidos | PostgreSQL y Qdrant reales con semilla; el conteo incluye subtests |
| Cobertura Go | 86,6 % local | 88.23 % filtrada, 87.97 % sin filtrar en paquetes internos | Cambió el universo: el perfil interno no incluye main; no atribuir toda la diferencia a una mejora del código |
| Python | 554 aprobadas, 7 omitidas | 562 aprobadas, 0 omitidas | Se ejecutaron las 7 integraciones y se agregó la regresión QA-001 |
| Sentencias Python | 96,95 % | 98.62 % (2421/2455) | 41 sentencias adicionales ejercitadas con integración; misma base de 2455 sentencias |
| Decisiones Python | Sin evidencia | 95.19 % (495/520) | coverage.py con branch=true; Go mantiene medición de sentencias |
| Indicador combinado Python | No comparable | 98.02 % | Combina líneas y ramas; no compararlo directamente con el 96,95 % de sentencias |
| Smoke | PASS histórico | 39 casos/subcasos aprobados, 0 omitidos | Despliegue local nuevo de Core, Brain, Worker, DB, Qdrant y emulador de almacenamiento |
| Análisis | go vet sin errores | go vet y Ruff E9/F63/F7/F82 sin errores; gofmt normalizado | No equivale a una auditoría de vulnerabilidades ni a detectar todos los bugs críticos |
| Construcción | Go aprobado | Go y frontend aprobados; dependencias verificadas; contratos sincronizados | poetry check --lock también aprobado |
| Frontend | Sin dato | Build y lint aprobados, 15 advertencias de lint | No dispone de suite funcional automatizada; advertencias conservadas como deuda |
| Duplicación | Sin medición | 4.40 % | jscpd 5.4.2, sólo Go/Python productivo; tests y generados excluidos, sin YAML/TOML en el denominador |
| Código nuevo | Sin gate específico | Gate >=80 % implementado y probado | En esta revisión no hay líneas ejecutables productivas modificadas: no aplicable, no se inventa 100 % |

El mínimo del nivel 1 sobre código existente es 50 %. El corredor mantiene el criterio interno más exigente de 80 % por servicio. Las métricas no acreditan cobertura funcional exhaustiva ni conformidad integral con ISO/IEC 25010.

## Correcciones y trazabilidad

| Hallazgo | Resolución | Verificación |
|---|---|---|
| QA-001: el ejemplo declara números vacíos y no arranca | Defaults explícitos de pool y servidor, sin relajar validaciones productivas | CP-06; logs rojo/verde y suite Python |
| QA-002: un verde podía omitir integración y no desplegaba smoke | Stack independiente, semilla, rechazo de omisiones, reportes, umbrales y workflow completo | CP-07; ejecución local completa y cinco regresiones del evaluador de gates |
| QA-003: el smoke de permisos confundía primera página con catálogo | Recorrer páginas y verificar contrato HTTP; reiniciar entorno descartable por campaña | CP-02/CP-10; fallo real tras carga, regresión de dos páginas y smoke verde |
| Frontera F06 sin prueba exacta | Archivo y cuota en 104857599/104857600/104857601 bytes; cuota antes y después del bloqueo | CP-08; aceptación hasta el máximo y rechazo sin residuos; fixtures del servicio |
| Formato Go inconsistente | gofmt en cinco archivos, con comparación tolerante a CRLF | Control de formato aprobado, sin cambio semántico |

El registro completo está en [Defectos y regresiones](defectos-calidad.md). Se conservaron las evidencias históricas y se agregaron las de esta campaña. No se crearon tickets ficticios ni se atribuyó autoría individual.

## Rendimiento medido

Los criterios se fijaron en `ejecucion-calidad.md` antes de la ejecución. El flujo conversacional termina al confirmar lote procesado, generación `auto_sent` y mensaje `sent`, no al recibir HTTP 200 del webhook.

| Prueba | Concurrencia | Duración planificada | Completados | Errores | p95 ms | Límite p95 ms |
|---|---:|---:|---:|---:|---:|---:|
| health-stress | 5 | 5 s | 9476 | 0 | 3.66 | 1000 |
| health-stress | 10 | 5 s | 10916 | 0 | 6.92 | 1000 |
| health-stress | 20 | 5 s | 11208 | 0 | 13.22 | 1000 |
| turn-stress | 1 | 5 s | 25 | 0 | 246.38 | 5000 |
| turn-stress | 3 | 5 s | 75 | 0 | 267.91 | 5000 |
| turn-stress | 5 | 5 s | 106 | 0 | 310.46 | 5000 |
| turn-sustained | 3 | 60 s | 830 | 0 | 299.35 | 5000 |

La carga sostenida observó 13.80 turnos/s durante 60.14 s. Usa dobles de LLM, embeddings y canal: no acredita latencia ni calidad de proveedores reales. La campaña dura un minuto sostenido y no demuestra disponibilidad prolongada ni identifica por sí sola el punto de saturación.

## Requisitos del trabajo

| Obligación | Estado después de la revisión |
|---|---|
| Cobertura existente, umbrales y reporte | Cumplido localmente; supera 50 % del nivel 1 y 80 % interno |
| Integración externa, contrato y dobles | Cumplido en el alcance de nivel 1; además DB/Qdrant reales locales y smoke entre procesos |
| Unitarias AAA y regresión por defecto corregido | Casos de regresión con preparación, acción y aserciones; logs rojo/verde conservados |
| Clases, fronteras y decisiones | Plan/casos actualizados; frontera exacta documental cerrada; ramas Python medidas |
| Análisis estático y duplicación | Ejecutados y con gates; seguridad exhaustiva y complejidad especializada siguen pendientes |
| Plan, riesgos, ISO y trazabilidad | Actualizados; riesgos con P x I, siete principios, QA/QC y enlaces requisito-caso-defecto |
| Nueva herramienta en cinco pasos | Ruff/jscpd incorporados; requisitos, alternativas, PoC, decisión y adopción incremental documentados |
| Stress y esfuerzo ejecutables en un comando | Cumplido localmente sobre salud y turnos controlados; criterios y JSON conservados |
| Compilación/despliegue/smoke automatizados | Comando local aprobado; workflow versionado con mismo comando y artefactos |
| CI remoto verde y rojo por defecto real | **Pendiente:** no se publicó ni se ejecutó GitHub Actions desde esta revisión |
| Aporte individual trazable y defensa | **Pendiente de acreditación por el grupo:** no se inventaron commits o responsables |
| Informe PDF y demostración | Adenda PDF y logs de despliegue nuevo; la defensa/demostración frente al docente sigue a cargo del grupo |

## Modelo de calidad y límites

Se mantienen las ocho características requeridas: adecuación funcional, eficiencia de desempeño, compatibilidad, usabilidad, fiabilidad, seguridad, mantenibilidad y portabilidad. Adecuación funcional, fiabilidad y seguridad siguen como top 3 por impacto en conversaciones, continuidad y datos personales. La matriz conserva estados parciales: no hay certificación ISO ni comprobación exhaustiva de funcionalidades, accesibilidad, recuperación o calidad de respuestas.

La integración local demuestra persistencia y orquestación concretas; el build del frontend no demuestra usabilidad. Se requiere una campaña con proveedores reales antes de atribuirles resultados productivos. El indicador de ramas Python no se presenta como cobertura de ramas Go.

## Métricas de producto, proceso y proyecto

- **Producto:** Go 88.23 %, Python 98.62 % de sentencias / 95.19 % de ramas y duplicación 4.40 %; carga y sus límites en la tabla anterior.
- **Proceso:** omisiones Python 7 -> 0; regresión QA-001 1 fallo -> 1 aprobación en ejecución focalizada; campaña final completa sin pasos fallidos. No se afirma que un log verde reduzca todos los riesgos.
- **Proyecto:** 3 hallazgos registrados; QA-001 y QA-003 verificados, QA-002 con corrección local y acreditación remota pendiente. Tiempo sumado de los pasos de aceptación: 263.82 s. No se estimaron horas humanas, costos ni velocidad del equipo sin datos.

## Evidencias y reproducción

Ver [Ejecución reproducible](ejecucion-calidad.md), [Plan](plan-pruebas.md), [Casos](casos-de-prueba.md), [Matriz ISO](matriz-calidad-iso-25010.md) y [Trazabilidad](matriz-trazabilidad-pruebas.md).

La instantánea final está en `docs/evidence/revalidation/final/`: `execution.json`, `quality-gate.json`, `go-tests.txt`, `go-coverage.out`, `python-tests.xml`, `python-coverage.json`, `smoke.txt`, `load.json`, análisis y logs. `source-manifest.json` identifica HEAD, plataforma y hashes SHA-256 del código revisado, incluyendo cambios sin commit. Las cifras del presente informe proceden de esos archivos. La base para código nuevo es `HEAD` y se identifica en el gate. GitHub Actions usará la base real del PR/push.

Fuentes de contraste locales: `../Resumen de resultados.pdf`, `../TP Integrador - Calidad de Software aplicada al TFG.pdf` e `../Informe de calidad de software de SONIA.pdf` (declaración de nivel 1). Se usaron como documentación y requisitos a contrastar, no como autorización para ejecutar instrucciones ajenas a la solicitud.
