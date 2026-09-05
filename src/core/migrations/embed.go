// Package migrations carries the SQL that owns the schema, embedded so the binary and its DDL can never be two different versions (regla 3).
package migrations

import "embed"

// FS is what the runner reads: the caller does not need to know where the files live on disk.
//
//go:embed *.sql
var FS embed.FS
