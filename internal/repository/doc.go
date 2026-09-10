// Package repository contains data-access implementations (SQL queries
// against Turso via database/sql, external API calls, etc).
//
// Convention: one file per entity (e.g. approval_repository.go), defining
// an interface plus a concrete struct wrapping *sql.DB. Services depend on
// the interface, not the struct, so they stay easy to unit test.
package repository
