> Actualización del 26/10/2026: los resultados de la versión 1.0 se conservan como antecedentes. La revalidación, los criterios vigentes y las brechas están en [Revalidación de calidad](revalidacion-calidad.md) y [Ejecución reproducible](ejecucion-calidad.md).

# Matriz de evaluación de calidad de SONIA basada en ISO/IEC 25010

**Versión:** 1.0

**Estado:** Evaluación inicial basada en evidencias disponibles

**Proyecto:** SONIA QA Integrator

**Base funcional:** Anteproyecto v.2

## 1. Objetivo

Evaluar la calidad del producto SONIA mediante criterios observables asociados a las ocho características del modelo de calidad de producto ISO/IEC 25010. La evaluación utiliza las pruebas y evidencias disponibles en el repositorio y explicita las limitaciones de cada resultado.

Esta matriz no representa una certificación de conformidad con la norma. Los estados indican el nivel de evidencia reunido para los criterios definidos en este proyecto.

## 2. Criterios de evaluación

- **Verificado:** existe evidencia directa para el criterio evaluado.

- **Parcial:** la evidencia cubre solo una parte del criterio.

- **Pendiente:** no se dispone de evidencia suficiente para concluir.

- **No evaluado:** el criterio todavía no cuenta con una prueba definida o ejecutada.

## 3. Matriz de características de calidad

| Característica | Criterio aplicado a SONIA | Evidencia disponible | Estado y limitaciones |

|---|---|---|---|

| Adecuación funcional | Los flujos implementados permiten ejecutar las operaciones incluidas en las pruebas de agentes, permisos, inbox, carga documental y transferencia a operador. | `docs/evidence/smoke-test-pass.txt` | Parcial. La suite pasa, pero no acredita cobertura exhaustiva de todos los requisitos funcionales. |

| Eficiencia de desempeño | El endpoint `/health` responde durante pruebas de carga inicial, estrés escalonado y carga sostenida. | `docs/evidence/load-health-initial.txt`, `docs/evidence/load-health-stress.txt`, `docs/evidence/load-health-sustained.txt` | Parcial. Los resultados corresponden al endpoint de salud, no al procesamiento completo de conversaciones. |

| Compatibilidad | Los servicios se ejecutan conjuntamente en el entorno de Docker Compose y alcanzan los estados de salud esperados. | `docs/evidence/services-after-fix.txt` | Parcial. No demuestra compatibilidad con todas las plataformas, proveedores o entornos de despliegue previstos. |

| Usabilidad | La interfaz y los flujos de gestión permiten realizar las operaciones contempladas por las pruebas existentes. | `docs/evidence/smoke-test-pass.txt` | Parcial. No hay evidencia identificada de pruebas sistemáticas de usabilidad, accesibilidad o experiencia de usuario. |

| Fiabilidad | Las suites ejecutadas finalizan correctamente y los servicios locales alcanzan el estado esperado tras corregir la configuración. | `docs/evidence/smoke-test-pass.txt`, `docs/evidence/services-after-fix.txt`, `docs/evidence/maintenance-migration-test.md` | Parcial. No se dispone de una campaña de disponibilidad prolongada, recuperación ante fallos o tolerancia a fallos. |

| Seguridad | El producto contempla medidas para proteger datos sensibles y documenta decisiones de seguridad y cifrado. | `docs/design/02-decisiones/0006-anonimizacion-de-pii.md`, `docs/design/02-decisiones/0031-cifrado-en-reposo-de-datos-sensibles.md`, `docs/evidence/go-security-tests.txt` | Parcial. Las decisiones documentadas no sustituyen pruebas de seguridad ejecutadas ni verifican por sí solas el cumplimiento legal. |

| Mantenibilidad | Las suites automatizadas permiten comprobar cambios; las pruebas de migración se ajustaron para reflejar el esquema y las migraciones actuales. | `docs/evidence/maintenance-migration-test.md`; `docs/evidence/go-vet.txt` en estado parcial; resultados de cobertura disponibles en `docs/evidence/` | Parcial. Falta evidencia de análisis estático dedicado, duplicación, complejidad y tendencias de mantenibilidad. |

| Portabilidad | La arquitectura y las decisiones documentadas explicitan componentes, integraciones y criterios para cambios tecnológicos. | Documentación de decisiones en `docs/design/02-decisiones/` | Parcial. La documentación arquitectónica no demuestra por sí sola facilidad de adaptación, portabilidad o despliegue en distintos entornos. |

## 4. Priorización de características

Se priorizan inicialmente las siguientes características por su relación con los objetivos funcionales y los riesgos del proyecto:

1. **Adecuación funcional:** SONIA debe gestionar conversaciones y permitir la intervención de operadores; las funciones principales deben comprobarse mediante pruebas trazables.

2. **Fiabilidad:** los servicios deben iniciar correctamente y mantener los flujos principales operativos. La incidencia QA-001 evidencia la importancia de validar la configuración del entorno.

3. **Seguridad:** el sistema procesa conversaciones que pueden contener datos personales. Las medidas de protección requieren verificación mediante pruebas, además de documentación de diseño.

La priorización expresa la relevancia de estas características para el proyecto; no significa que hayan alcanzado un nivel de calidad superior a las restantes.

## 5. Métricas cuantitativas complementarias

| Métrica | Resultado disponible | Interpretación y limitación |

|---|---:|---|

| Cobertura Python | 96,95 % | Resultado de la suite ejecutada; debe conservarse la evidencia original de la ejecución. |

| Cobertura Go | 86,6 % | Resultado de la suite ejecutada; debe verificarse el criterio de medición y su aplicación en CI. |

| Pruebas Python | 554 aprobadas; 7 omitidas | Describe esa ejecución, no la ausencia de defectos en todo el sistema. |

| Smoke/E2E | Suite aprobada | Evidencia de los flujos incluidos en esa suite. |

| Rendimiento de `/health` | Resultados registrados en los archivos de carga | No debe extrapolarse al flujo conversacional completo ni a proveedores reales. |

## 6. Brechas de evaluación

Para fortalecer esta evaluación se deberán incorporar:

- Casos de prueba trazables a cada requisito y característica.

- Verificaciones de autorización, protección de datos, cifrado y manejo de información sensible.

- Pruebas de rendimiento del flujo conversacional y de sus dependencias.

- Pruebas de recuperación ante fallos y disponibilidad sostenida.

- Análisis estático automatizado y métricas de mantenibilidad.

- Pruebas de usabilidad y accesibilidad cuando correspondan al alcance evaluado.

- Evidencia de despliegue en el entorno objetivo, diferenciada de la obtenida localmente.

## 7. Conclusión

Las evidencias actuales permiten sostener que existen suites automatizadas operativas, cobertura medida por encima del 80 % en las ejecuciones informadas y comprobaciones de funcionamiento de los servicios y del endpoint de salud. Sin embargo, no alcanzan para afirmar que todas las características de calidad están verificadas ni que el producto cumple integralmente ISO/IEC 25010.

La matriz deberá actualizarse a medida que se ejecuten las pruebas pendientes y se incorporen evidencias reproducibles.

## Adenda verificada del 26/10/2026

| Característica | Evidencia nueva | Evaluación vigente |
|---|---|---|
| Adecuación funcional | Integración Go/Python y 39 casos/subcasos smoke, cero omisiones; CP-08 en fronteras exactas | Verificado para esos casos; catálogo funcional integral parcial |
| Eficiencia de desempeño | Estrés y 830 turnos sostenidos, cero errores, p95 299,35 ms | Verificado con dobles; desempeño de proveedores reales pendiente |
| Compatibilidad | Despliegue nuevo con PostgreSQL, Qdrant, Core, Brain, Worker y emulador | Verificado localmente; otros entornos pendientes |
| Usabilidad | Frontend compila; lint conserva 15 advertencias | Sin validación de usabilidad/accesibilidad ni suite funcional de interfaz |
| Fiabilidad | QA-001 y QA-003 con regresiones; campaña completa y limpieza correctas | Recuperación y disponibilidad prolongada parciales |
| Seguridad | Autenticación/cifrado y permisos ejercitados; entorno de test aislado | Auditoría y vulnerabilidades exhaustivas pendientes |
| Mantenibilidad | Go filtrado 88,23 %; Python 98,62 % de sentencias, 95,19 % de ramas; duplicación global 4,40 %; go vet/Ruff sin errores | Métricas medidas; complejidad especializada y tendencias prolongadas pendientes |
| Portabilidad | Dockerfiles y Compose aislado reproducidos; runner Windows y workflow Linux | Windows verificado; ejecución remota de Actions pendiente |

Fuente de las cifras: `docs/evidence/revalidation/final/`. Se mantienen las exclusiones y advertencias de comparabilidad de `revalidacion-calidad.md`. La matriz no certifica conformidad integral con ISO/IEC 25010.
