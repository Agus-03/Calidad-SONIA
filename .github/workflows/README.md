# Integración continua de calidad

El workflow `ci.yml` ejecuta el mismo comando de aceptación local documentado en [Ejecución de calidad](../../docs/ejecucion-calidad.md). Construye y despliega el proyecto aislado `sonia-quality`, migra, siembra y exige cero omisiones de integración y smoke. Incluye análisis estático, compilación del backend y frontend, contratos, cobertura por servicio y código nuevo, duplicación, estrés y esfuerzo sostenido.

Los resultados se publican con `actions/upload-artifact` incluso ante fallos. Los tests Go corren con detector de carreras en CI. La base de comparación de código nuevo se toma del PR o del commit anterior al push. El umbral es 80 % por servicio y sobre líneas ejecutables modificadas; la duplicación máxima es 5 % sobre producción Go/Python. Las exclusiones están documentadas y no se agregan para ocultar fallos.

La reproducción local y sus resultados no sustituyen una ejecución de GitHub Actions. Antes de declarar completa la entrega, adjuntar enlaces a una ejecución remota aprobada y a una fallida por un defecto real. Las credenciales del entorno aislado son exclusivamente fixtures de test; ningún servicio real de IA, WhatsApp o Firebase necesita claves.
