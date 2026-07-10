package migrations

import "embed"

// FS contains SQL migrations embedded into the bot binary.
//
//go:embed *.sql
var FS embed.FS
