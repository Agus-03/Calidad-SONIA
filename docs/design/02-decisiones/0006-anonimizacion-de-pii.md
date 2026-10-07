# ADR-0006 — Anonimización de PII antes del envío al LLM externo

| Campo | Valor |
|---|---|
| **Estado** | Aceptada · **§2 enmendado por [ADR-0042](0042-pipeline-de-decision-del-turno.md)** (decisión D6): los marcadores que recibe el LLM son **tres** y no cuatro, porque el patrón de once dígitos emite el genérico `[CUIT/CUIL]` —la primera de las dos salidas que la nota de este ADR dejó abiertas—. La otra mitad de esa nota, desambiguar al persistir, sigue abierta · **§2 y §3 acotados por [ADR-0066](0066-datos-personales-en-los-documentos.md)** (2026-10-03): la redacción cubre la conversación con la clienta; el contenido de los documentos de la base de conocimiento sale sin redactar, bajo un aviso que acepta quien los sube |
| **Fecha** | 2026-08-06 · §2 enmendado 2026-09-25 (Fase 8) |
| **Etapa** | Diseño — Paso 3 |
| **Resuelve** | Contradicción entre la sección 5.2 y el modelo de datos |
| **Requisitos afectados** | F04, F31 · secciones 5.2 y 7.3 |

---

## 1. Contexto

La sección 5.2 del anteproyecto declara, como uno de los **seis problemas fundacionales del proyecto**:

> *"Riesgos de seguridad en datos sensibles: las conversaciones comerciales frecuentemente incluyen datos personales (DNI, CUIT, CBU, domicilio). Los sistemas actuales **no ofrecen mecanismos de anonimización o filtrado previo al envío al modelo externo**, exponiendo información sensible en violación a normativas de protección de datos (Ley 25.326)."*

El modelo de datos no tiene ni un atributo ni una entidad que refleje ese filtrado. Concretamente: si un cliente escribe *"te paso mi CBU 0170099220000067797151 para la seña"*, ese texto entra al prompt y **sale hacia el proveedor de LLM externo** (la sección 8.4 contempla GPT-4o, Claude o Cohere).

El trabajo estaría denunciando en la página 4 exactamente la fuga que produciría en la implementación.

## 2. Decisión

**Se resuelve en el diseño.** Antes de que cualquier texto salga hacia el proveedor de LLM externo, los datos personales identificables se reemplazan por un **marcador de tipo**.

> **Acotado por [ADR-0066](0066-datos-personales-en-los-documentos.md) el 2026-10-03.** *"Cualquier texto"* se lee como el texto de la conversación con la clienta, que es lo que denuncia la sección 5.2. El contenido de los documentos de la base de conocimiento no se redacta: viaja al proveedor de embeddings y al del LLM tal como viene, y quien sube cada documento acepta un aviso que lo dice.

- **Datos alcanzados: DNI, CUIT, CUIL y CBU.** Los cuatro se almacenan cifrados.
- **El LLM nunca recibe el valor.** Recibe el marcador: `[DNI]`, `[CUIT]`, `[CUIL]`, `[CBU]`.
- **El valor real se cifra y persiste del lado propio** (F31), asociado al contacto.
- **`generations.prompt_rendered` guarda la versión redactada**, no la original.

Ejemplo del texto tal como lo recibe el modelo:

```
Hola, soy [NOMBRE], DNI [DNI], quiero alquilar el depto de Jesús María.
Te paso mi CBU [CBU] para la seña.
```

## 3. Justificación

- **Coherencia del trabajo.** No se puede denunciar la fuga en la formulación del problema y producirla en la solución. Es la contradicción más fácil de encontrar leyendo el documento.
- **Es un diferenciador real** frente a Chatwoot y Respond.io, y la sección 6.3 lo declara parte del gap tecnológico que define el aporte original.
- **El modelo no pierde capacidad.** Para entender la intención —"te paso mi CBU para la seña"— el marcador alcanza y sobra. El número no aporta nada al razonamiento del agente.
- **Cifrar no es anonimizar**, y conviene no confundirlos: F31 ya cifra en reposo (`dni_encrypted`, `cbu_encrypted`), lo que protege la base de datos. Esto protege el dato **en tránsito hacia un tercero**, que es un problema distinto y es el que denuncia 5.2.
- **`prompt_rendered` debe guardar lo redactado** o el registro de auditoría se convertiría en el mayor repositorio de datos sensibles del sistema — el peor resultado posible partiendo de una buena intención. *(Acotado por [ADR-0066](0066-datos-personales-en-los-documentos.md): vale para lo que escribió la clienta. Los fragmentos de documentos que recupera el RAG entran a `prompt_rendered` tal como vienen.)*

## 4. Consecuencias

### Positivas

- La sección 5.2 queda respaldada por el diseño, no sólo enunciada.
- Reduce la superficie de exposición ante la Ley 25.326 a lo que vive dentro del perímetro propio.
- Da material concreto para el capítulo de resultados: tasa de detección, falsos positivos y falsos negativos son todos medibles.

### Negativas / costos asumidos

- **Un componente más en la ruta caliente de cada turno**, con su latencia.
- **Riesgo de falso negativo.** Un dato mal formateado —con puntos, espacios, o escrito en palabras— puede escapar a la detección. **Se asume, y conviene medirlo y declararlo en el informe**: presentar el mecanismo como infalible sería peor que no tenerlo, porque invita a confiar de más.
- **Riesgo de falso positivo:** un número de once dígitos que no es un CUIT se redacta igual. Impacto menor — el agente pierde un dato que probablemente no necesitaba.
- Hay que fijar el momento exacto de la redacción: **antes** de armar el prompt, no dentro de él.

> **Nota sobre CUIT y CUIL.** El anteproyecto usa **CUIT** en 5.2 y **CUIL** en 7.3 y en el Anexo C (`cuil_encrypted`). Son identificadores distintos —CUIT para entidades con inscripción fiscal, CUIL para trabajadores en relación de dependencia— y **los dos se almacenan cifrados**, porque en un negocio de alquileres aparecen ambos.
>
> Pero comparten longitud y formato (once dígitos), así que **un redactor que trabaja sobre texto libre no puede distinguirlos**: viendo `20-35412887-4` en un mensaje, no hay forma de saber cuál de los dos es. Dos salidas posibles, a resolver en el Paso 5: emitir un marcador genérico para el patrón de once dígitos (y desambiguar recién al persistir, donde el contexto suele indicarlo), o unificar en un campo con discriminador de tipo. La decisión afecta al modelo, no a esta decisión.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **Sacar la anonimización de la formulación del problema (5.2)** | Era una salida legítima y más barata: el trabajo dejaría de prometerlo y no habría contradicción. Se descarta porque el mecanismo es central al aporte declarado en 6.3, y renunciar a él debilitaría el argumento del gap tecnológico. |
| **Confiar en el cifrado en reposo de F31** | No resuelve nada: el dato viaja en claro dentro del texto de la conversación antes de llegar a la base. |
| **Redactar sólo al persistir, no al enviar** | Invierte el problema: protege nuestra base y expone al tercero, que es exactamente lo contrario de lo que denuncia 5.2. |
