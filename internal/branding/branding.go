// Package branding is the single source of truth for the product's name,
// domains, and contact addresses. Inject these constants instead of
// hard-coding the product name so the wordmark stays swappable.
//
// NOTE: this is the user-facing brand only. Infrastructure identifiers that
// would break existing deployments if changed are deliberately NOT derived
// from here — see the comments at:
//   - internal/auth/session.go / internal/web/csrf/csrf.go (cookie names)
//   - internal/apikey/apikey.go                            (the "so_" prefix)
//   - fly.toml                                             (app/volume names)
package branding

const (
	// ProductName is the user-facing name of the service.
	ProductName = "Dokaz"

	// Slug is the lowercase machine form used for download filenames and the
	// observability service name. Keep it in sync with dashboards/*.yml.
	Slug = "dokaz"

	// DomainSite is the marketing/docs domain (host only, no scheme).
	DomainSite = "dokaz.io"
	// DomainApp is the application domain (host only, no scheme).
	DomainApp = "app.dokaz.io"

	// EmailFrom is the default transactional sender address.
	EmailFrom = "notifications@dokaz.io"
	// SupportEmail is the support contact address.
	SupportEmail = "support@dokaz.io"
	// SalesEmail is the sales contact address.
	SalesEmail = "sales@dokaz.io"
	// LegalEmail is the legal/terms contact address.
	LegalEmail = "legal@dokaz.io"
	// PrivacyEmail is the privacy/data-protection contact address.
	PrivacyEmail = "privacy@dokaz.io"
	// SecurityEmail is the vulnerability-disclosure contact address.
	SecurityEmail = "security@dokaz.io"

	// TOTPIssuer is the label shown in authenticator apps.
	TOTPIssuer = ProductName
)
