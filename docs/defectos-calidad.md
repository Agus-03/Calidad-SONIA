# Registro de defectos y regresiones

## QA-001 - El ejemplo distribuido impide arrancar Brain y Worker

| Campo | Registro |
|---|---|
| Requisito | Configuración reproducible, fiabilidad y contingencia del entorno de aceptación |
| Caso | CP-06, `test_example_environment_can_boot_brain_and_worker` |
| Severidad / prioridad | Alta / alta: impide iniciar servicios y verificar la integración |
| Pasos | Cargar todas las variables de `.env.example`, completar DSN, token, clave documental, bucket y dobles autorizados en test; construir `Settings` y validar Worker |
| Esperado | Configuración válida con puerto 8000 y pool 1/10; Worker sin problemas de configuración |
| Obtenido antes | Tres errores de conversión a entero: `BRAIN_HTTP_PORT`, `DB_POOL_MIN_SIZE`, `DB_POOL_MAX_SIZE` vacíos |
| Causa | Los valores vacíos del archivo de ejemplo sustituyen los defaults válidos de Pydantic |
| Corrección | Defaults explícitos de entorno, servidor y pool en `.env.example`; los secretos obligatorios siguen requiriendo configuración |
| Rojo / verde | `evidence/revalidation/qa-001-red.txt` / `qa-001-green.txt`; regresión incluida además en la suite completa |
| Estado | Corregido y verificado en el repositorio; amplía la corrección local documentada previamente |

No se toleran silenciosamente números inválidos en producción. El test usa secretos de prueba y no consulta el `.env` personal.

## QA-002 - La aceptación podía aprobar con integraciones omitidas y sin smoke

| Campo | Registro |
|---|---|
| Requisito | TP: integración externa comprobada, smoke después del despliegue y reportes automáticos |
| Caso | CP-07, `scripts/quality.py`, comprobaciones de omisiones Go y JUnit Python |
| Severidad / prioridad | Alta / alta: un verde incompleto reduce la confianza en la entrega |
| Reproducción | Ejecutar la suite Python sin `SONIA_TEST_DATABASE_URL`; inspeccionar el workflow anterior sin despliegue/smoke ni artefactos de resultados |
| Esperado | La aceptación completa debe preparar dependencias y datos, ejecutar integración/smoke y rechazar cualquier omisión |
| Obtenido antes | 554 pruebas aprobadas y 7 omitidas; código de salida satisfactorio y 96,95 % de cobertura |
| Causa | La omisión es deliberada en las suites para desarrollo, pero CI no preparaba ni exigía todas las precondiciones de Brain y de smoke |
| Corrección | Stack aislado, migraciones y semilla explícitas; suites internas y smoke separadas, rechazo de omisiones, cobertura, carga y artefactos en CI |
| Evidencia | `evidence/revalidation/python-baseline.txt` frente a los reportes de la campaña completa; workflow versionado |
| Estado | Corrección verificada localmente. Falta publicar y adjuntar ejecuciones remotas de Actions |

La omisión unitariamente no es un defecto del código productivo. El defecto es aceptarla como validación completa del despliegue.

## QA-003 - El smoke de permisos suponía que todo el catálogo cabía en una página

| Campo | Registro |
|---|---|
| Requisito | F15/F30, CP-02: alcance de contactos por rol y repetibilidad del smoke |
| Severidad / prioridad | Media / alta para la confianza en la aceptación |
| Reproducción | Ejecutar una campaña conversacional que cree más de 100 contactos; repetir el smoke de contactos sobre esa base |
| Esperado | El administrador encuentra ambos contactos de la fixture recorriendo las páginas; el operador sólo encuentra el asignado |
| Obtenido antes | El smoke consulta solamente `per_page=100`, pierde los contactos de la fixture y falla aunque la API pagine correctamente |
| Causa | Aserción que confundía una página del catálogo con el catálogo completo |
| Corrección | Recorrido paginado con validación del contrato HTTP y regresión de dos páginas; nueva campaña con base descartable limpia |
| Rojo / verde | `evidence/revalidation/qa-003-red.jsonl`, `qa-003-green.txt` y smoke de la ejecución completa |
| Estado | Regresión unitaria verificada; ver el resultado completo en el informe de revalidación |

## Mantenimiento y brechas distinguidos de defectos

- Cinco archivos no cumplían gofmt; se aplicó únicamente formato. Los finales de línea de Windows se normalizan al comparar, evitando reescribir todo el repositorio.
- Un primer intento de integración sin semilla produjo una referencia de cuenta inválida y omisiones; con migraciones y semilla aplicadas la suite pasó. Se corrigió la preparación reproducible, sin alterar la lógica de contactos para ocultar el error.
- CP-08: F06 tiene pruebas de 104.857.599, 104.857.600 y 104.857.601 bytes para archivo individual y cuota acumulada. En la cuota se repiten antes y después del bloqueo. Se verifican aceptación hasta el máximo y rechazo sin objetos/documentos residuales. Son pruebas del servicio con dobles, no una carga E2E de un PDF de 100 MiB.
- CP-09: estrés y esfuerzo sostenido comprueban generación enviada en la base, sin contar el simple acuse del webhook como éxito. Alcance y umbrales en `ejecucion-calidad.md`.
- `scripts/test_quality_gate.py` comprueba umbrales exactos, cobertura insuficiente, duplicación excesiva, código nuevo sin cubrir y numeración de líneas del diff. Sus datos son sintéticos para probar el mecanismo; no se presentan como métricas de SONIA.

## Plantilla para próximos defectos

ID; título; requisito; caso detector; versión/entorno; pasos y datos; esperado; obtenido; severidad; prioridad; causa; corrección; evidencia roja; regresión; evidencia verde; responsable; estado. La matriz requisito-caso-defecto debe enlazar estos registros. El registro versionado funciona como la herramienta de seguimiento admitida por el TP; no se afirma que exista un ticket en Jira.
