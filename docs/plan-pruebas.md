\# Plan de pruebas de SONIA



\*\*Versión:\*\* 1.0

\*\*Estado:\*\* Plan inicial basado en las pruebas y evidencias disponibles al 25/10/2026.

\*\*Proyecto:\*\* SONIA — Plataforma de atención conversacional asistida por inteligencia artificial.



\## 1. Propósito



Establecer el alcance, los niveles, los criterios de aceptación, los riesgos y las evidencias de las pruebas de SONIA. El plan busca verificar la corrección funcional, la integración entre componentes, la estabilidad del sistema y la calidad del software, diferenciando los resultados efectivamente obtenidos de las actividades todavía pendientes.



\## 2. Alcance



El sistema está compuesto por servicios desarrollados en Go y Python, una base de datos relacional, un servicio de búsqueda vectorial y servicios auxiliares. Las pruebas contemplan:



\- Pruebas unitarias de los componentes Go y Python.

\- Pruebas de integración entre componentes y dependencias.

\- Pruebas smoke y flujos end-to-end disponibles.

\- Comprobaciones de salud y disponibilidad de los servicios.

\- Pruebas de carga inicial, estrés escalonado y carga sostenida sobre el endpoint `/health`.

\- Verificación de configuración, migraciones y arranque de servicios.

\- Análisis de cobertura y revisión de las verificaciones incorporadas en CI.



Este plan no considera demostrada la carga del flujo conversacional real a partir de las pruebas ejecutadas sobre `/health`. Tampoco considera demostrada la calidad de respuestas del modelo en producción mediante pruebas realizadas con dobles de LLM y embeddings.



\## 3. Estrategia y niveles de prueba



\### 3.1. Pruebas unitarias



Se ejecutan pruebas automatizadas sobre los componentes de Python y Go. Los resultados registrados son:



\- \*\*Python:\*\* 554 pruebas aprobadas y 7 omitidas; cobertura global informada del 96,95 %.

\- \*\*Go:\*\* suite completa aprobada; cobertura global informada del 86,6 %.

- **Evidencia Go:** `docs/evidence/go-coverage.txt`, reporte generado mediante `go tool cover -func=cobertura.out`.



Las pruebas omitidas no se contabilizan como aprobadas. La cobertura global no implica que cada módulo alcance individualmente el mismo porcentaje.



\### 3.2. Pruebas de integración



Se verifica la interacción entre los servicios y sus dependencias. La suite Go ejecutada incluye los paquetes disponibles en el repositorio. Las pruebas que requieren una base de datos de prueba pueden omitirse cuando no está configurada la variable `SONIA\_TEST\_DATABASE\_URL`; por ello, una ejecución global aprobada no prueba por sí sola que todos los escenarios de integración con base de datos hayan sido ejecutados.



Las migraciones se verificaron mediante la prueba de esquema inicial y la comprobación de las migraciones aplicadas. El mantenimiento de sus expectativas está documentado en `docs/evidence/maintenance-migration-test.md`.



\### 3.3. Pruebas smoke y end-to-end



Se ejecutaron las pruebas disponibles de smoke y de flujos integrados. El resultado registrado fue satisfactorio. La evidencia está en `docs/evidence/smoke-test-pass.txt`.



Esta comprobación valida los flujos cubiertos por la suite ejecutada; no implica que todas las funcionalidades del anteproyecto estén cubiertas ni que se haya completado una validación integral en producción.



\### 3.4. Pruebas de rendimiento



Se ejecutaron pruebas sobre el endpoint de salud `/health`, con tres modalidades:



\- \*\*Carga inicial:\*\* 50 solicitudes concurrentes distribuidas entre 5 trabajadores; 50 respuestas HTTP 200.

\- \*\*Estrés escalonado:\*\* niveles de concurrencia de 5, 10 y 20, durante 10 segundos por nivel. No se registraron respuestas distintas de HTTP 200 en las ejecuciones documentadas.

\- \*\*Carga sostenida:\*\* duración aproximada de 60 segundos, con 5 trabajadores y 31.236 solicitudes registradas; todas respondieron HTTP 200.



Los resultados detallados están en `docs/evidence/load-health-initial.txt`, `docs/evidence/load-health-stress.txt` y `docs/evidence/load-health-sustained.txt`.



Estas pruebas evalúan la respuesta del endpoint de salud bajo las condiciones indicadas. No permiten extrapolar directamente la capacidad del sistema para procesar conversaciones, invocar modelos de lenguaje, recuperar documentos o persistir mensajes.



\## 4. Entorno de ejecución



El entorno local de integración utiliza Docker Compose e incluye los servicios de SONIA y sus dependencias. En la ejecución documentada se verificaron el estado saludable de los servicios, la respuesta HTTP 200 del endpoint de salud y el arranque del cerebro y del trabajador.



La configuración de dobles de LLM y embeddings debe identificarse al interpretar resultados: las mediciones obtenidas con esos dobles no representan necesariamente la latencia, disponibilidad ni comportamiento de los proveedores reales.



No se deben incluir en las evidencias contraseñas, tokens, claves ni contenidos sensibles de variables de entorno.



\## 5. Criterios de entrada y salida



\### 5.1. Criterios de entrada



Antes de ejecutar una prueba se debe verificar:



\- Código y dependencias disponibles en la versión bajo evaluación.

\- Configuración del entorno y variables requeridas, sin exponer secretos.

\- Servicios y dependencias necesarios iniciados.

\- Datos de prueba válidos y aislados de información real sensible.

\- Criterios de aceptación definidos para el caso.

\- Registro de la versión, comando y configuración relevante de la ejecución.



\### 5.2. Criterios de salida



Una ejecución se considera satisfactoria cuando:



\- Los casos previstos finalizaron y sus resultados quedaron registrados.

\- No existen fallos sin analizar que impidan aceptar el alcance evaluado.

\- Los fallos conocidos tienen un registro, severidad y decisión de tratamiento.

\- La evidencia permite reproducir o revisar el resultado.

\- Las pruebas omitidas y las limitaciones del entorno quedan explícitas.



La aprobación de una suite no sustituye la verificación de que los escenarios requeridos fueron efectivamente ejecutados.



\## 6. Riesgos de calidad



| Riesgo | Probabilidad | Impacto | Tratamiento |

|---|---|---|---|

| Variables de entorno vacías invalidan la configuración de servicios | Media | Alto | Validar configuración de arranque y documentar valores requeridos |

| Pruebas de integración se omiten por falta de dependencias configuradas | Media | Alto | Registrar omisiones y ejecutar integración en un entorno preparado |

| Las pruebas de `/health` se interpretan como rendimiento conversacional | Alta | Alto | Diferenciar explícitamente endpoint y carga evaluados |

| Los dobles de LLM y embeddings no representan proveedores reales | Alta | Alto | Ejecutar pruebas controladas con proveedores reales antes de afirmar resultados de producción |

| Cobertura global oculta módulos insuficientemente probados | Media | Alto | Revisar cobertura por servicio y por módulo |

| Falta de trazabilidad entre requisitos, casos y defectos | Alta | Medio | Crear una matriz de trazabilidad específica de QA |

| La canalización CI no aplica todas las puertas de calidad esperadas | Media | Alto | Incorporar y verificar análisis estático, umbrales y reportes automatizados |



Las probabilidades e impactos de esta tabla son estimaciones iniciales de planificación, no frecuencias estadísticas observadas.



\## 7. Registro de evidencias



| Identificador | Evidencia | Propósito |

|---|---|---|

| EV-01 | `docs/evidence/smoke-test-pass.txt` | Resultado de smoke y flujos E2E disponibles |

| EV-02 | `docs/evidence/load-health-initial.txt` | Prueba de carga inicial de `/health` |

| EV-03 | `docs/evidence/load-health-stress.txt` | Prueba de estrés escalonado de `/health` |

| EV-04 | `docs/evidence/load-health-sustained.txt` | Prueba de carga sostenida de `/health` |

| EV-05 | `docs/evidence/maintenance-migration-test.md` | Ajuste y verificación de expectativas de migración |

| EV-06 | `docs/evidence/defect-config-brain-worker.md` | Registro del defecto de configuración QA-001 |

| EV-07 | `docs/evidence/services-after-fix.txt` | Estado de los servicios después de la corrección |

| EV-08 | `docs/evidence/brain-worker-after-fix.txt` | Evidencia de arranque del cerebro y trabajador |



La cobertura de Python y Go se encuentra registrada en los resultados de ejecución disponibles. Para que los porcentajes sean reproducibles desde el repositorio, se debe conservar también el comando exacto, la versión evaluada y el reporte generado en cada ejecución.



\## 8. Defectos y mantenimiento de pruebas



El defecto QA-001 documenta un problema de configuración local relacionado con variables vacías que anulaban valores predeterminados y provocaban errores de arranque. La corrección se verificó en el entorno local y quedó documentada en `docs/evidence/defect-config-brain-worker.md`.



El ajuste de las expectativas de las pruebas de migración se registra como mantenimiento de pruebas, no como defecto funcional confirmado del sistema.



\## 9. Trazabilidad



`docs/design/01-trazabilidad.md` contiene trazabilidad de requisitos y diseño. No reemplaza una matriz de pruebas.



Queda pendiente mantener una matriz específica con, como mínimo, los campos siguientes:



| Requisito | Caso de prueba | Resultado | Evidencia | Defecto asociado |

|---|---|---|---|---|

| Identificador del requisito | Identificador del caso | Pendiente de vinculación | Ruta del artefacto | Identificador o N/A |



No se asignan identificadores de casos ni resultados a requisitos individuales hasta verificar su correspondencia con pruebas concretas.



\## 10. Actividades pendientes



1\. Completar la matriz de trazabilidad requisito → caso de prueba → defecto.

2\. Documentar casos individuales con precondiciones, pasos, resultado esperado y resultado obtenido.

3\. Incorporar y ejecutar análisis estático automatizado para los lenguajes del proyecto.

4\. Revisar la configuración del pipeline CI para confirmar umbrales de cobertura y puertas de calidad.

5\. Generar reportes de cobertura exportables y conservarlos como artefactos de ejecución.

6\. Diseñar y ejecutar pruebas de carga sobre el flujo conversacional real con datos de prueba válidos.

7\. Evaluar las integraciones externas y los flujos relevantes con una configuración representativa del entorno objetivo.

8\. Documentar resultados, limitaciones y criterios de aceptación de cada actividad pendiente.



\## 11. Conclusión



Las ejecuciones registradas aportan evidencia favorable sobre las suites unitarias disponibles, los flujos smoke/E2E cubiertos y la respuesta del endpoint de salud bajo las cargas documentadas. Sin embargo, el alcance de esas evidencias es limitado: quedan actividades necesarias para completar la trazabilidad de QA, fortalecer las puertas automáticas de calidad y evaluar el comportamiento bajo carga del procesamiento conversacional real.



Por tanto, este documento constituye el plan inicial de pruebas y el registro de su estado conocido; no implica que todas las actividades de calidad estén completas ni que se haya demostrado el cumplimiento integral de todos los requisitos del proyecto.
