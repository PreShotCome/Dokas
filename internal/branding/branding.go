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
	ProductName = "Vesta"

	// Slug is the lowercase machine form used for download filenames and the
	// observability service name. Keep it in sync with dashboards/*.yml.
	Slug = "vesta"

	// DomainSite is the marketing/docs domain (host only, no scheme).
	DomainSite = "vesta.io"
	// DomainApp is the application domain (host only, no scheme).
	DomainApp = "app.vesta.io"

	// EmailFrom is the default transactional sender address.
	EmailFrom = "notifications@vesta.io"
	// SupportEmail is the support contact address.
	SupportEmail = "support@vesta.io"
	// SalesEmail is the sales contact address.
	SalesEmail = "sales@vesta.io"
	// LegalEmail is the legal/terms contact address.
	LegalEmail = "legal@vesta.io"
	// PrivacyEmail is the privacy/data-protection contact address.
	PrivacyEmail = "privacy@vesta.io"
	// SecurityEmail is the vulnerability-disclosure contact address.
	SecurityEmail = "security@vesta.io"

	// TOTPIssuer is the label shown in authenticator apps.
	TOTPIssuer = ProductName
)
