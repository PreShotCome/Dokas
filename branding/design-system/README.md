# Dokaz Design System

**Backup verification you can independently prove.** Dokaz periodically restores your database dumps into an isolated sandbox, runs assertions against the restored data, and produces signed evidence anyone can verify without trusting Dokaz. This design system captures the brand, foundations, components, and product surfaces for building Dokaz interfaces and assets.

The product's differentiator is a **four-link evidence chain** — hashed input (SHA-256), in-sandbox restore + receipts, a detached Ed25519 signature, and an open-source verifier. The design language exists to make that proof feel *concrete and trustworthy*: real numbers, monospace receipts, expected-vs-actual tables — never just a green checkmark.

## Sources

This system was reverse-engineered from the Dokaz codebase. Explore these to build higher-fidelity Dokaz designs:

- **GitHub — [PreShotCome/Dokas](https://github.com/PreShotCome/Dokas)** — the Go monolith (`app.dokaz.net`): Templ + HTMX + Tailwind marketing/dashboard web app, the Flutter mobile app under `app/`, brand SVGs under `branding/`, and the Tailwind color config + `assets/css/input.css` that define the palette. The marketing site lives in a separate (unprovided) repo.

Pulled into this project: brand marks (`branding/`), the geometric favicon, color tokens (from `tailwind.config.js` + Flutter `VS` palette), and product copy/structure (from the Templ landing templates and Flutter screens).

---

## Content Fundamentals

**Voice: direct, technical, and unflinching.** Dokaz talks to engineers and the people who get them audited (SOC 2 / HIPAA / cyber-insurance). It never hypes; it states facts and backs them with mechanism.

- **Person:** Second-person "you" / "your backups"; the product is "Dokaz" (never "we" in marketing headlines, though "we never touch your production database" appears in explanatory body). Imperative for actions: "Connect a database", "Run drill", "Collect signed evidence".
- **The signature move — name the gap, then close it.** Headlines call out a false sense of safety; body delivers the proof. *"Your backups are untested until something restores them." → "…proves the data really came back, and signs the result."* And the manifesto line: **"Proof, not a green checkmark."**
- **Concrete over adjectival.** Prefer real artifacts and numbers: "Ed25519-signed PDF", "seven-year retention", "row_count, table_exists, no_nulls", "2m 14s", "4.2M rows". Avoid vague superlatives ("powerful", "seamless", "next-gen").
- **Casing:** Sentence case for headings and buttons ("Start free", "View pricing", "Run drill"). UPPERCASE only for tracked eyebrows ("AUDITOR-GRADE EVIDENCE", "HOW IT WORKS") and status verdicts in the app ("PASSED" / "FAILED"). Status pills are lowercase ("passed", "running", "down").
- **Monospace is a voice, not just a font.** Hashes, signatures, IDs (`drill_a1f9c2e4`), SQL, file commands (`shasum -a 256 dump.tar`), and assertion values are always set in mono — it signals "this is verifiable machine truth".
- **No emoji.** Status is carried by colored dots, checks, and the semantic palette — never emoji. Icons are thin line glyphs.
- **Reassurance through specificity.** Trust copy lists frameworks by name ("SOC 2 · ISO 27001 · HIPAA · Cyber-insurance renewals") and states boundaries plainly ("It never needs a connection to your live database").
- **Examples of tone:**
  - Hero sub: *"…so a broken backup shows up in a report, not an outage."*
  - Feature: *"Go past 'it restored': assert row counts, table and column existence, and non-null constraints on the restored data."*
  - CTA: *"Stop guessing about your backups."*
  - App login tagline: *"Know the moment a backup fails."*

---

## Visual Foundations

**Mood: cool, calm, professional — beach-meets-audit-room.** The turtle mark ("your data is held, protected by its shell") sets a steady, unhurried tone. The palette is dark, steely, and oceanic; nothing is loud.

- **Theme:** **Dark-only.** `<html class="dark">` is hard-set in the product. There is no light mode. Design on charcoal.
- **Color:**
  - *Brand blue* (`--blue-500 #5b8def` web / `--blue-400 #6e9bf0` app) — baby-royal-blue. Primary actions, links, the turtle's shell. The full ramp 50→900.
  - *Pink* (`--pink-400 #f48fb1`) — accent. The turtle's central scute, promotional badges ("Most popular"), eyebrows, the "running" state, and a faint background glow. Used sparingly; it's the warm note against all the cool.
  - *Sea-teal* (`--teal-400 #56c596`) — "healthy / verified / passed / up". This is the success color (not a generic green); it reads cool and oceanic, reinforcing the beach feel. Carries every positive verdict.
  - *Steel* (`#1b2027`→`#e5ecf2`) — neutrals. Charcoal page base, card surfaces, dividers, icons, body/muted text.
  - *Danger* (`#ff6b81`) — a pink-red for failed/down, harmonising with the pink accent rather than a jarring pure red. *Warning* is amber `#f0b429` for drafts/notices.
- **Backgrounds:** The signature surface is **steely charcoal with two soft radial glows** — a cool blue from the top-left (`12% -8%`) and a fainter pink from the top-right (`100% -4%`) over a `#252c35 → #1b2027` linear gradient (`--bg-app`). Fixed attachment. The hero adds a **masked dot-grid** texture (22px radial dots, faded to the edges with a radial mask) for depth. No photography; no illustration beyond the turtle mark. No noise/grain.
- **Type:** **Geist** for everything (display + body) — neutral, modern, professional; semibold (600) for headings with tight tracking (`-0.02em`), regular for body at ~1.6 line-height. **JetBrains Mono** is load-bearing for all verifiable artifacts. (Substitution note: the product itself ships system-ui + ui-monospace; we standardise on these Google Fonts for consistent rendering — see Iconography/fonts caveat below.)
- **Corner radii:** Soft but not pill-y. `md 8px` on buttons/inputs, `lg 12px` on app tiles (Flutter uses 12), `xl 16px` on web cards, `2xl 24px` / `3xl 32px` on hero & CTA panels. `full` only for status pills and badges.
- **Cards:** Steel fill (`--surface-card #2e3640`), 1px hairline border (`--border #33405a`), `xl` radius, a subtle `shadow-sm`. On hover (interactive cards only) the border brightens to `--border-strong` and the shadow deepens to `shadow-lg` — a quiet lift, no scale.
- **Shadows & elevation:** Deep but low-opacity (dark backgrounds swallow soft shadows). The real "lift" on primary actions is a **brand-tinted glow** (`--glow-brand`), not a grey drop shadow.
- **Buttons:** Primary is a **vertical brand gradient** (`blue-500 → blue-700`) with a brand glow that brightens and rises 1px on hover, then presses flat on click — it reads as a physical button, not a flat fill. Ghost = transparent with a faint steel tint on hover. Secondary = steel card surface + border.
- **Hover / press states:** Hover = lighten (brighter gradient / faint tint) + optional −1px translate. Press = settle back to translateY(0) and a smaller shadow. Links lighten toward `--blue-300`. No color inversions, no big scale jumps.
- **Motion:** Calm and short. `120–320ms`, standard ease (`cubic-bezier(0.2,0,0,1)`); fades and 1px lifts, **no bounce, no infinite loops**. HTMX-style poll-driven step updates in the app feel like ticking, not animating.
- **Borders & dividers:** 1px hairlines. Within cards/tables, use the even-fainter `--border-subtle` (rgba) for row separators. Evidence cards use a *dashed* divider under the header to evoke a receipt/certificate.
- **Transparency & blur:** Used lightly — the floating view-switcher and mobile tab bar use `rgba` surfaces with `backdrop-filter: blur`. Status-pill backgrounds are low-alpha tints of their semantic hue.
- **Layout:** Generous vertical rhythm on marketing (sections ~`space-24` apart); dense, scannable tables in the app. Content max-widths: prose `42rem`, content `64rem`, wide `80rem`. App is a fixed 230px sidebar + fluid main.
- **Imagery vibe:** Cool, dark, synthetic. The only recurring visual is the turtle mark and faux product chrome (a browser/phone frame with a drill or evidence card inside). Everything skews blue/steel with pink and teal as the two accents.

---

## Iconography

- **Style:** Thin **line icons**, ~1.8–1.9px stroke, rounded caps and joins, 24px grid — drawn inline as SVG `<path>`s in the product (no icon font, no sprite). This system mirrors that: line glyphs for nav (drills/databases/heartbeats/evidence/settings), a custom rounded **check** for passes and pipeline steps, and a **shield-check** for signed evidence. Match Lucide/Feather geometry if you need more.
  - *Substitution flag:* The Dokaz repo hand-rolls its SVG icons in Templ; there is no shipped icon set to import. If you need a broad icon library, **Lucide** (same stroke weight + rounded style) is the closest CDN match — flag it when you use it.
- **Status as iconography:** A colored **dot** (teal/pink/danger/steel) is the primary status signal, paired with the pill label. Checks (teal) and crosses (danger) mark assertion/step results.
- **Emoji / unicode:** **None.** Verdicts use `✓` / `✕` glyphs inside colored chips, not emoji. The "+" on FAQ accordions and "·" separators are the only decorative unicode.
- **The mark — a turtle:** Blue hexagonal shell (its scutes echo the six pipeline steps), steel head + four legs, a single pink central scute. Variants in `branding/`:
  - `mark.svg` / `icon.svg` — full-color turtle (primary mark).
  - `mark-mono.svg` — monochrome, for single-color contexts.
  - `assets/static/favicon.svg` — a geometric hex-shell-with-checkmark reduction (favicon / small sizes).
  - `mark-128/64/512.png` — raster fallbacks.
  - ⚠️ `branding/lockup.svg` in the repo is a **stale amber** version (red→yellow) that predates the current blue/pink brand — **do not use it.** Build lockups from `mark.svg` + a Geist "Dokaz" wordmark (see `guidelines/brand-lockup.html`).

---

## Index / Manifest

**Root**
- `styles.css` — the single entry point consumers link. `@import`s only.
- `tokens/` — `fonts.css`, `colors.css`, `typography.css`, `spacing.css`, `effects.css`.
- `branding/`, `assets/static/` — logos, marks, favicons (imported from the repo).
- `SKILL.md` — Agent-Skill manifest for using this system in Claude Code.

**Foundations** (`guidelines/` — specimen cards in the Design System tab)
- Colors: blue, pink, sea-teal, steel, semantic+surfaces · Type: display, body & eyebrow, mono, scale · Spacing: scale, radii, elevation & glow · Brand: lockup, mark, background.

**Components** (`components/` — React primitives, namespace `window.DokazDesignSystem_<hash>`)
- `core/` — **Button** (primary/secondary/ghost/danger), **Card**, **Badge** (neutral/brand/accent/teal/outline).
- `forms/` — **Input** (label/hint/error).
- `feedback/` — **StatusPill** (passed/failed/running/pending/up/down/verified/draft).
- `evidence/` — **StepChip** (pipeline stage) and **AssertionRow** (kind / expected / actual / pass — the "proof, not a checkmark" primitive).

**UI kits** (`ui_kits/`)
- `web/` — `dokaz.net` marketing landing + the `app.dokaz.net` dashboard (drills list → drill evidence detail, heartbeats). Toggle Marketing/App in the top-right.
- `mobile/` — the Flutter app: sign-in → drills → drill evidence → heartbeats, in a phone frame.

---

### Caveats
- **Fonts substituted.** The product uses the OS system sans + ui-monospace (no bundled webfont). This system standardises on **Geist** + **JetBrains Mono** from Google Fonts for consistent rendering. Swap in a licensed brand face if you have one.
- **Icons are hand-rolled** in the codebase; this system recreates the key glyphs inline and recommends **Lucide** as the nearest CDN library if you need more.
- **Marketing site repo not provided** — the landing recreation is built from the in-app Templ landing templates, which is the canonical marketing layout in the monolith.
- UI kits are cosmetic recreations (self-contained, not wired to the bundle) so they render and verify standalone; the real `components/` primitives compile into `_ds_bundle.js` for consumers.
