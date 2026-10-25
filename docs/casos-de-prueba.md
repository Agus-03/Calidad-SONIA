\# Casos de prueba de SONIA



\*\*Versión:\*\* 1.0

\*\*Estado:\*\* Registro inicial basado en pruebas smoke ejecutadas

\*\*Evidencia principal:\*\* `docs/evidence/smoke-test-pass.txt`



\## 1. Propósito



Documentar escenarios de prueba funcionales y de integración ejecutados sobre SONIA, incluyendo sus condiciones, pasos, resultados esperados, resultados observados y trazabilidad con los objetivos del anteproyecto.



Los resultados registrados corresponden al entorno local de pruebas. No deben interpretarse automáticamente como validación en producción.



\## 2. Criterios de registro



\- \*\*PASS:\*\* el registro de ejecución indica que el test pasó.

\- \*\*Pendiente de ampliación:\*\* la ejecución pasó, pero todavía debe completarse alguna información para la ficha formal.

\- \*\*No demostrado:\*\* la evidencia disponible no permite afirmar el cumplimiento del requisito completo.



Los pasos se describen a partir de la implementación disponible. No se inventan resultados que no estén respaldados por las aserciones o el registro de ejecución.



\## 3. Casos de prueba



\### CP-01 — Rechazo de un modelo sin tarifa



\*\*Objetivo:\*\* comprobar que la creación de un agente rechace un modelo de lenguaje que no figura con tarifa válida.



\*\*Tipo:\*\* integración / funcional.



\*\*Precondiciones:\*\*

\- Servicios de Core y Brain disponibles.

\- Cuenta y administrador de prueba creados.

\- Sesión autenticada con token válido.



\*\*Pasos:\*\*

1\. Crear un agente con un proveedor y modelo que figuren en la lista de modelos con tarifa.

2\. Enviar una solicitud para crear otro agente con un modelo inexistente en esa lista.

3\. Inspeccionar el código HTTP y el código de rechazo de la respuesta.



\*\*Resultado esperado:\*\*

\- El agente con modelo reconocido se crea con HTTP `201`.

\- El modelo sin tarifa se rechaza con HTTP `400` y código `unpriced\_model`.



\*\*Resultado observado:\*\* PASS, según `smoke-test-pass.txt`.



\*\*Trazabilidad:\*\* control de validación de configuración del agente. Relación funcional con la configuración del agente del proyecto.



\*\*Evidencia de implementación:\*\* `src/core/smoke/agents\_test.go`.



\*\*Pendiente:\*\* ampliar la ficha con la evidencia de respuesta y el requisito funcional específico asociado.



\### CP-02 — Control de acceso a contactos y campos sensibles



\*\*Objetivo:\*\* verificar que el acceso a contactos y a determinados campos dependa del rol del operador.



\*\*Tipo:\*\* integración / seguridad funcional.



\*\*Precondiciones:\*\*

\- Core disponible.

\- Datos de prueba preparados para los roles operador, supervisor y administrador.

\- Tokens válidos para los roles utilizados.



\*\*Pasos:\*\*

1\. Consultar como operador un contacto al que tiene acceso.

2\. Verificar que la respuesta no incluya `dni`, `cuit`, `cuil` ni `cbu`, tampoco bajo el valor de CBU utilizado en la prueba.

3\. Consultar el mismo contacto como supervisor y administrador.

4\. Verificar que esos roles reciban el campo CBU de prueba.

5\. Consultar el listado de contactos como operador y comprobar que incluya el contacto de una conversación propia, pero no uno fuera de su alcance.

6\. Consultar el listado como administrador y comprobar que incluya ambos contactos.

7\. Intentar modificar el nombre como operador.

8\. Intentar modificar el campo `notes` y verificar el rechazo `403`, código `field\_not\_writable`.

9\. Enviar un campo no reconocido, `apodo`, y verificar el rechazo `400`, código `unknown\_field`.



\*\*Resultado esperado:\*\* acceso limitado al alcance del operador, ocultación de los campos indicados, edición autorizada del nombre y rechazo de las modificaciones no permitidas.



\*\*Resultado observado:\*\* PASS, según `smoke-test-pass.txt`.



\*\*Trazabilidad:\*\* OE5, por la plataforma operativa, la gestión de contactos y los roles. También aporta evidencia funcional relacionada con la protección de datos.



\*\*Evidencia de implementación:\*\* `src/core/smoke/contacts\_test.go`.



\*\*Limitación:\*\* esta prueba no demuestra por sí sola el cumplimiento legal integral de la Ley 25.326 ni la seguridad de todos los endpoints.



\### CP-03 — Carga y procesamiento de documentos de la base de conocimiento



\*\*Objetivo:\*\* verificar el flujo de carga de un documento para un agente y su procesamiento posterior por el worker.



\*\*Tipo:\*\* integración.



\*\*Precondiciones:\*\*

\- Core disponible.

\- Almacenamiento de documentos y Qdrant accesibles.

\- Clave de cifrado disponible en el entorno de pruebas.

\- Cuenta de prueba y administrador autenticable.

\- Agente de prueba creado.



\*\*Pasos:\*\*

1\. Consultar el aviso de carga de documentos.

2\. Comprobar que la respuesta incluya una versión válida.

3\. Preparar un PDF de prueba con contenido de ejemplo.

4\. Cargar el archivo en la base de conocimiento del agente.

5\. Comprobar que la solicitud de carga devuelva HTTP `201`.

6\. Verificar el procesamiento posterior del documento por el worker, incluida la fragmentación y la vectorización.

7\. Comprobar el rechazo de una nueva carga del mismo archivo.

8\. Verificar el comportamiento de eliminación del documento según las aserciones del test.



\*\*Resultado esperado:\*\* carga aceptada, procesamiento e indexación por el worker, rechazo del duplicado y comportamiento de eliminación coherente con las reglas implementadas.



\*\*Resultado observado:\*\* PASS, según `smoke-test-pass.txt`, incluidos los subtests de fragmentación/vectorización y rechazo de archivo duplicado.



\*\*Trazabilidad:\*\* OE6, como evidencia parcial del pipeline RAG.



\*\*Evidencia de implementación:\*\* `src/core/smoke/documents\_test.go`.



\*\*Limitaciones:\*\* este escenario no demuestra el límite de 100 MB por agente ni que el 90 % de las consultas de dominio se resuelva sin escalada.



\### CP-04 — Transferencia explícita a un operador humano



\*\*Objetivo:\*\* comprobar que una solicitud explícita de atención humana interrumpa la generación automática y deje la conversación pendiente para un operador.



\*\*Tipo:\*\* integración / extremo a extremo.



\*\*Precondiciones:\*\*

\- Core y Brain disponibles.

\- Datos de prueba de cliente y conversación.

\- Secreto de firma de webhook configurado.



\*\*Pasos:\*\*

1\. Enviar un webhook válido que contenga una solicitud explícita de atención humana.

2\. Esperar a que se procese el evento y consultar el estado persistido.

3\. Comprobar que el estado de la conversación sea `waiting`.

4\. Comprobar que el modo del agente sea `off`.

5\. Verificar que exista un único evento de transferencia, asociado a la señal `explicit\_request` y marcado como activado.

6\. Comprobar que no se haya creado ninguna generación de respuesta automática.

7\. Comprobar que no se hayan enviado mensajes salientes.

8\. Verificar que el registro de clasificación identifique `explicit\_request` como motivo de la transferencia.



\*\*Resultado esperado:\*\* conversación pendiente de atención humana, bot desactivado, transferencia registrada y ninguna respuesta automática generada o enviada.



\*\*Resultado observado:\*\* PASS, según `smoke-test-pass.txt`.



\*\*Trazabilidad:\*\* OE3, por la transición IA → humano. Es evidencia funcional del flujo, pero no mide la precisión de clasificación sobre el conjunto de 200 conversaciones exigido por el anteproyecto.



\*\*Evidencia de implementación:\*\* `src/core/smoke/transfer\_test.go`.



\### CP-05 — Rechazo de webhook con firma inválida



\*\*Objetivo:\*\* verificar que un webhook con firma incorrecta no produzca un mensaje procesable.



\*\*Tipo:\*\* integración / seguridad.



\*\*Precondiciones:\*\*

\- Core disponible.

\- Secreto válido de firma configurado para el inbox de prueba.



\*\*Pasos:\*\*

1\. Preparar un identificador único de webhook.

2\. Enviar el webhook con una firma inválida.

3\. Verificar que el endpoint devuelva HTTP `200`.

4\. Consultar el registro del evento y comprobar que `signature\_valid` sea falso y el estado sea `discarded`.

5\. Consultar la tabla de mensajes y comprobar que no exista ningún mensaje con ese identificador.



\*\*Resultado esperado:\*\* evento registrado como descartado, firma inválida y cero mensajes almacenados para ese webhook.



\*\*Resultado observado:\*\* PASS, según `smoke-test-pass.txt`.



\*\*Trazabilidad:\*\* control de integridad y autenticidad de los webhooks del canal de integración.



\*\*Evidencia de implementación:\*\* `src/core/smoke/turn\_test.go`.



\*\*Limitación:\*\* el resultado corresponde al caso de firma inválida probado; no acredita por sí solo la seguridad completa de la integración con WhatsApp Business API.



\## 4. Resumen de resultados



| Indicador | Resultado |

|---|---|

| Casos documentados en esta versión | 5 |

| Casos con ejecución PASS registrada | 5 |

| Evidencia de ejecución | `docs/evidence/smoke-test-pass.txt` |

| Entorno | Local de pruebas |

| Validación de producción real | No demostrada por estos casos |

| Cumplimiento cuantitativo integral de OE3, OE6 y OE7 | No demostrado por estos casos |



\## 5. Pendientes



1\. Vincular cada caso con el identificador exacto del requisito funcional del catálogo del proyecto.

2\. Incorporar evidencia individual de respuestas y aserciones cuando sea necesaria para una auditoría.

3\. Documentar casos negativos y de límites para carga de documentos, autenticación, permisos y manejo de errores.

4\. Agregar pruebas que evalúen el límite de 100 MB y la tasa de respuestas RAG sin escalada.

5\. Medir la precisión de transferencia sobre un conjunto de prueba de 200 conversaciones.

6\. Mantener separada la evidencia local de la validación en producción con el cooperante.



\### Brecha identificada: valores límite de la base de conocimiento



\*\*Requisito relacionado:\*\* F06 — carga y administración de documentos de la base de conocimiento, con un máximo acumulado de 100 MiB por agente.



\*\*Límite definido en el código:\*\* `MaxKnowledgeBaseBytes = 100 << 20`, equivalente a 104.857.600 bytes.



\*\*Evidencia verificada:\*\* la ejecución de `go test ./internal/documents -run "Limit|Empty|Upload" -count=1 -v` finalizó con `PASS`. Se verificaron la carga válida, el rechazo de un archivo superior al límite, el control del tamaño acumulado, la revalidación bajo bloqueo y el rechazo de nombres inválidos. Los tests de servicio utilizan dependencias simuladas.



\*\*Brecha pendiente:\*\* no se identificó una prueba específica que verifique la aceptación de un archivo de exactamente 104.857.600 bytes ni el rechazo de uno de 104.857.601 bytes. El test existente rechaza un archivo claramente superior al máximo, pero no demuestra por sí solo el comportamiento exacto en ambos puntos de corte.



\*\*Decisión de testing:\*\* no se modifica el código de producción ni se agregan por ahora pruebas que requieran reservar aproximadamente 100 MiB. La brecha queda registrada para una futura prueba específica de valores límite.



\*\*Estado:\*\* cobertura parcial; el comportamiento general está probado, pero los dos valores de frontera exactos siguen pendientes de verificación.
