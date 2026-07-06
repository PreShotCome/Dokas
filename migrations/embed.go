// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package migrations embeds the SQL migration files into the binary so they
// can be applied without a migrations directory on disk. Both the migrate CLI
// (cmd/migrate) and the server's startup migration step read from this FS,
// which makes a deployed binary fully self-contained — no separate release
// step and no files to ship alongside it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
