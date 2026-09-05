# ADR-0019 — Almacenamiento de objetos: Supabase Storage

| Campo | Valor |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 2026-09-04 |
| **Etapa** | Diseño — Paso 7 (nivel físico) |
| **Resuelve** | **H-31** — `documents` registra que existe un archivo de hasta 100 MB y no dónde está |
| **Requisitos afectados** | F06 · ADR-0005, ADR-0010, ADR-0017 (decisión *c*) |

---

## 1. Contexto

`documents` no tiene ruta, clave de objeto ni columna binaria. El modelo registra que existe un archivo de hasta 100 MB (F06) y **no dónde vive**.

No alcanza con un directorio local: **el que recibe la subida y el que la fragmenta son procesos distintos** —el núcleo en Go recibe el upload, el `worker` de Python lee los bytes para chunkear e indexar—, y ADR-0017 los declara desplegables separados. Un directorio local ataría los dos a la misma máquina.

El Paso 8 dejó tres salidas evaluadas: almacenamiento de objetos, volumen compartido y `bytea`.

## 2. Decisión

**Supabase Storage**, usado **exclusivamente** como almacén de objetos — no como base de datos, no como proveedor de autenticación.

- Se agregan `documents.object_key` y `documents.locked_at` al catálogo, **al firmar ADR-0017**, que es donde ya estaban declarados.
- El núcleo recibe la subida, escribe el objeto y crea la fila completa. El `worker` **lee** los bytes por el puerto de almacenamiento y sólo actualiza `status`, `locked_at` y `error_message` — una columna, un servicio, sin traspaso de propiedad.
- El proveedor queda detrás de `adapters/storage/`, que es la **cuarta frontera con puerto** declarada por ADR-0017.

## 3. Justificación

- **`bytea` infla los backups y el WAL** con archivos de 100 MB, sobre la misma base que sostiene toda la trazabilidad de OE2.
- **Un volumen compartido ata los dos desplegables a la misma máquina**, que contradice la partición de ADR-0017 y su decisión de red aislada.
- **Usar Supabase sólo para esto evita arrastrar su base y su auth**, que duplicarían PostgreSQL y F18 — y la base única compartida es justamente lo innegociable de ADR-0017: con base por servicio, la cadena de trazabilidad de OE2 deja de ser un join.
- El puerto ya estaba previsto, así que cambiar de proveedor más adelante es escribir un adaptador.

## 4. Consecuencias

### Positivas

- PostgreSQL queda liviano: guarda metadatos y fragmentos, no archivos.
- Los dos desplegables acceden a los bytes por red, sin compartir disco.
- El proveedor es reemplazable sin tocar `worker/` ni `documents/`.

### Negativas / costos asumidos

- **Una dependencia externa más**, con su cuenta, sus credenciales y su modo de falla. Si Supabase no responde, la **ingesta** se detiene — el turno conversacional no, porque no toca el almacén.
- **La consistencia entre la fila y el objeto es eventual.** Hay que definir el orden de escritura y qué se hace con los objetos huérfanos si el `INSERT` falla después de subir. **Queda abierto.**
- **Los documentos del cooperante salen de nuestra infraestructura.** El Paso 12 (Ley 25.326, diagrama de flujo de datos) tiene que incluirlo, y **el cifrado de los documentos sigue sin decidirse** — era el pendiente vecino de H-31 y este ADR no lo cierra.
- Dos atributos nuevos en el catálogo, condicionados a la firma de ADR-0017.

## 5. Alternativas descartadas

| Alternativa | Motivo del descarte |
|---|---|
| **`bytea` en PostgreSQL** | Archivos de 100 MB inflan backups y WAL de la base que sostiene toda la evidencia de la tesina. |
| **Volumen compartido entre los dos servicios** | Ata núcleo y `worker` a la misma máquina y contradice la partición y el aislamiento de red de ADR-0017. |
| **Supabase completo como BaaS** (base + auth + storage) | Duplicaría PostgreSQL y F18, y rompería la base única compartida sobre la que se apoya la trazabilidad de OE2. |
