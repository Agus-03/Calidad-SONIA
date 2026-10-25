\# Registro de mantenimiento de pruebas: migraciones de base de datos



\*\*Tipo:\*\* Mantenimiento de prueba de integración.

\*\*Componente:\*\* `src/core/internal/platform/db/migrate\_integration\_test.go`



\## Situación detectada



La prueba `TestTheInitialSchemaApplies` esperaba 38 tablas en el esquema `public`, mientras que la base de datos de prueba presentaba 39. Además, `TestAppliedListsWhatRan` verificaba únicamente la migración `0001\_initial\_schema.sql`, aunque en la base de datos estaban registradas dos migraciones: `0001\_initial\_schema.sql` y `0002\_inbox\_operators.sql`.



\## Cambio realizado



Se actualizó la expectativa de la prueba para contemplar 39 tablas y se modificó la verificación del historial de migraciones para incluir ambas migraciones.



\## Verificación



La consulta a `schema\_migrations` confirmó que las dos migraciones estaban aplicadas. También se ejecutó la suite de pruebas de Go, que finalizó correctamente con el comando `go test -p 1 ./... -count=1`.



\## Alcance y limitaciones



Este cambio alinea las expectativas de las pruebas con el esquema observado. Por sí solo, no demuestra la ausencia de defectos en las migraciones ni constituye evidencia de un defecto funcional del producto.
