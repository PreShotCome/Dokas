// Copyright (c) 2026 Ian Lee. All rights reserved.
// Proprietary and confidential; use is governed by the LICENSE file at the
// repository root. Access to this source grants no license. See NOTICE.

// Package dokaz is the module root. It exists only to embed repository-root
// assets (currently CHANGELOG.md) so they can be served without copying the
// file into a sub-package or the container image.
package dokaz

import _ "embed"

// ChangelogMarkdown is the raw text of CHANGELOG.md, embedded at build time.
// The public /changelog page parses and renders it (see internal/changelog).
//
//go:embed CHANGELOG.md
var ChangelogMarkdown string
