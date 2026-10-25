\# Matriz de trazabilidad de pruebas de SONIA



\*\*Versión:\*\* 1.0

\*\*Estado:\*\* Inicial; pendiente de completar la trazabilidad de todos los requisitos y casos individuales.

\*\*Proyecto:\*\* SONIA QA Integrator

\*\*Base funcional:\*\* Anteproyecto v.2

\*\*Plan asociado:\*\* `docs/plan-pruebas.md`



\## 1. Propósito



Esta matriz vincula los objetivos y requisitos relevantes del proyecto con los casos de prueba ejecutados, las evidencias disponibles y los defectos identificados. Permite distinguir entre funcionalidades verificadas, verificaciones parciales y actividades pendientes.



La matriz se actualizará cuando se incorporen nuevas pruebas o se modifiquen los requisitos. La existencia de una evidencia solo demuestra el alcance concreto de la prueba ejecutada; no implica que se haya validado toda la funcionalidad relacionada.



\## 2. Criterios de estado



\- \*\*Verificado:\*\* existe una prueba ejecutada cuyo resultado respalda el requisito dentro del alcance indicado.

\- \*\*Parcial:\*\* hay evidencia relacionada, pero no cubre por completo el requisito.

\- \*\*Pendiente:\*\* falta una prueba específica o evidencia suficiente.

\- \*\*No aplica:\*\* el requisito no corresponde al alcance de la prueba considerada.



\## 3. Matriz de trazabilidad



| ID | Requisito u objetivo | Caso o verificación asociada | Evidencia | Defecto relacionado | Estado |

|---|---|---|---|---|---|

| TR-01 | OE5: plataforma operativa para gestión de conversaciones, contactos, roles e inbox | Suite de smoke y pruebas E2E de agentes, permisos, inbox y transferencia a operador | `docs/evidence/smoke-test-pass.txt` | No registrado en esta matriz | Parcial: la suite pasó, pero no demuestra por sí sola la cobertura funcional exhaustiva de OE5 |

| TR-02 | OE5: cobertura de pruebas unitarias igual o superior al 80 % | Ejecución de pruebas Python y medición de cobertura Go | `docs/evidence/go-coverage.txt` — capturas y resultados de cobertura existentes | No registrado en esta matriz | Verificado para los resultados medidos: Python 96,95 % y Go 86,6 %; pendiente confirmar que la medición y los umbrales se mantengan en CI |

| TR-03 | OE6: recuperación de información mediante RAG y base de conocimiento | Pruebas existentes de carga de documentos y flujos E2E relacionados | `docs/evidence/smoke-test-pass.txt` | No registrado en esta matriz | Parcial: no acredita por sí sola el límite de 100 MB por agente ni el 90 % de consultas de dominio sin escalada |

| TR-04 | OE7: despliegue operativo en un entorno real | Levantamiento de servicios y comprobación de salud de los contenedores | `docs/evidence/services-after-fix.txt`, `docs/evidence/brain-worker-after-fix.txt` | QA-001, documentado en `docs/evidence/defect-config-brain-worker.md` | Parcial: evidencia de funcionamiento del entorno local, no de producción real |

| TR-05 | OE7: tasa de resolución autónoma igual o superior al 75 % | Requiere evaluación de un conjunto definido de conversaciones y clasificación de resolución autónoma | Sin evidencia suficiente identificada | Pendiente | Pendiente |

| TR-06 | OE7: consistencia de tono igual o superior al 85 % | Requiere conjunto de respuestas evaluadas y criterio de puntuación reproducible | Sin evidencia suficiente identificada | Pendiente | Pendiente |

| TR-07 | Disponibilidad del servicio de salud | Solicitudes HTTP al endpoint `/health` bajo carga inicial, estrés escalonado y carga sostenida | `docs/evidence/load-health-initial.txt`, `docs/evidence/load-health-stress.txt`, `docs/evidence/load-health-sustained.txt` | No registrado en esta matriz | Verificado para `/health` en el entorno de prueba; no demuestra rendimiento del flujo conversacional completo |

| TR-08 | Integridad del esquema y del registro de migraciones | Pruebas de migración que comprueban el esquema inicial y las migraciones aplicadas | `docs/evidence/maintenance-migration-test.md` | Mantenimiento de pruebas, no defecto funcional confirmado | Verificado para las comprobaciones ejecutadas |

| TR-09 | Configuración correcta de Brain y Worker en el entorno local | Reproducción y corrección de variables de configuración vacías; comprobación posterior de servicios | `docs/evidence/defect-config-brain-worker.md`, `docs/evidence/services-after-fix.txt` | QA-001 | Corregido en el entorno local; no implica que se haya validado la configuración de todos los entornos |

| TR-10 | Integración y funcionamiento de los flujos principales de la aplicación | Ejecución de la suite smoke/E2E existente | `docs/evidence/smoke-test-pass.txt` | No registrado en esta matriz | Verificado para los flujos incluidos en esa ejecución |

| TR-11 | Análisis estático automatizado y criterios de calidad en CI | Revisar el pipeline actual y ejecutar las herramientas de análisis que se incorporen | No se identificó evidencia de un análisis estático dedicado en el inventario revisado | `go vet ./...` se ejecutó sin errores reportados, manteniendo pendiente la verificación del pipeline CI. | Pendiente |

| TR-12 | Pruebas de estrés y carga sostenida | Solicitudes concurrentes y sostenidas al endpoint `/health` | `docs/evidence/load-health-stress.txt`, `docs/evidence/load-health-sustained.txt` | No registrado en esta matriz | Verificado para el endpoint de salud; pendiente evaluar el flujo conversacional y sus dependencias reales |

| TR-13 | Validación de modelos: rechazo de un modelo sin tarifa | CP-01: creación de un modelo sin tarifa | `docs/evidence/smoke-test-pass.txt`; ficha `CP-01` en `docs/casos-de-prueba.md` | No registrado | Verificado para el escenario de rechazo probado; pendiente vincularlo con un requisito formal específico |

| TR-14 | OE5: control de acceso a contactos y protección de campos sensibles | CP-02: permisos por rol, visibilidad de datos sensibles y validación de campos modificables | `docs/evidence/smoke-test-pass.txt`; ficha `CP-02` en `docs/casos-de-prueba.md` | No registrado | Verificado para los escenarios incluidos en la prueba; no demuestra exhaustivamente todos los permisos de la plataforma |

| TR-15 | OE6: carga y procesamiento de documentos para la base de conocimiento | CP-03: carga de PDF, procesamiento, duplicados y eliminación | `docs/evidence/smoke-test-pass.txt`; ficha `CP-03` en `docs/casos-de-prueba.md` | No registrado | Parcial: no acredita el cumplimiento del 90 % de consultas de dominio ni los valores exactos del límite de 100 MiB |

| TR-16 | OE5: transferencia de conversaciones a un operador humano | CP-04: transferencia explícita, desactivación del bot y ausencia de respuesta automática | `docs/evidence/smoke-test-pass.txt`; ficha `CP-04` en `docs/casos-de-prueba.md` | No registrado | Verificado para el flujo de transferencia explícita probado; no demuestra todas las condiciones posibles de transferencia |

| TR-17 | Validación de integridad de webhooks entrantes | CP-05: rechazo de webhook con firma inválida | `docs/evidence/smoke-test-pass.txt`; ficha `CP-05` en `docs/casos-de-prueba.md` | No registrado | Verificado para el escenario de firma inválida; pendiente ampliar las pruebas a otros escenarios de seguridad de webhooks |



\### \*\*Observaciones sobre el OE6 - límite de 100MB por agente\*\*

\*\*Evidencia complementaria de límite:\*\* `src/core/internal/documents/model.go` define el máximo de 100 MiB; `src/core/internal/documents/service.go` valida el tamaño individual y el servicio vuelve a comprobar el límite acumulado bajo bloqueo. `src/core/internal/documents/service\_test.go` incluye pruebas de rechazo por exceso, acumulación y revalidación bajo bloqueo. La suite del paquete finalizó con `PASS`.



\*\*Limitación:\*\* no se verificaron explícitamente los valores de 104.857.600 y 104.857.601 bytes. La evidencia corresponde a pruebas unitarias con dependencias simuladas y no demuestra por sí sola el límite mediante una carga de extremo a extremo con almacenamiento real.



\*\*Estado de trazabilidad:\*\* implementación y cobertura parcial verificadas; frontera exacta pendiente.



\## 4. Limitaciones de las evidencias de rendimiento



Las pruebas de carga registradas se ejecutaron contra el endpoint `GET /health`. En el entorno inspeccionado, la configuración indicaba `environment=test`, `llm\_use\_double=True` y `embeddings\_use\_double=True`.



Por lo tanto, los resultados obtenidos permiten describir el comportamiento del endpoint de salud bajo las cargas probadas, pero no deben extrapolarse al rendimiento del procesamiento de conversaciones, a la recuperación RAG ni a la integración con proveedores reales de modelos y embeddings.



\## 5. Defectos y relación con los casos



El defecto QA-001 corresponde a variables de configuración vacías que impedían el arranque correcto de Brain y Worker en el entorno local. Se documentó la corrección y se verificó el estado posterior de los servicios.



La actualización de las expectativas de las pruebas de migración se registra como mantenimiento de pruebas. No debe clasificarse como defecto funcional sin evidencia adicional.



Para los próximos defectos se deberá registrar, como mínimo:



\- Identificador único.

\- Caso de prueba que lo detectó.

\- Requisito afectado.

\- Pasos para reproducirlo.

\- Resultado esperado y resultado observado.

\- Severidad, estado y evidencia.

\- Caso de regresión asociado cuando se corrija.



\## 6. Actividades pendientes



1\. Desglosar los requisitos relevantes en casos de prueba identificados individualmente.

2\. Vincular cada caso con su resultado de ejecución y evidencia concreta.

3\. Completar la matriz de calidad ISO/IEC 25010 con criterios, métricas y evidencias.

4\. Verificar qué pruebas de integración se ejecutan realmente en CI y cuáles se omiten por falta de servicios externos.

5\. Incorporar evidencia de análisis estático automatizado y criterios de calidad en el pipeline.

6\. Ejecutar pruebas de rendimiento sobre flujos conversacionales con dependencias y datos de prueba controlados.

7\. Definir protocolos reproducibles para medir la resolución autónoma y la consistencia del tono.
