---
name: dokaz-design
description: Use this skill to generate well-branded interfaces and assets for Dokaz, either for production or throwaway prototypes/mocks/etc. Contains essential design guidelines, colors, type, fonts, assets, and UI kit components for prototyping.
user-invocable: true
---

Read the README.md file within this skill, and explore the other available files.
If creating visual artifacts (slides, mocks, throwaway prototypes, etc), copy assets out and create static HTML files for the user to view. If working on production code, you can copy assets and read the rules here to become an expert in designing with this brand.
If the user invokes this skill without any other guidance, ask them what they want to build or design, ask some questions, and act as an expert designer who outputs HTML artifacts _or_ production code, depending on the need.

Quick orientation:
- **Dark-only brand.** Steely charcoal surfaces (`--bg-app`), baby-royal-blue primary, pink accent, sea-teal for "verified/passed", danger pink-red. Never design a light mode.
- **Tokens** live in `tokens/*.css`, all reachable via `styles.css`. Link that one file.
- **Voice** is direct and technical: name the gap, then close it with proof. Monospace (JetBrains Mono) for all hashes/signatures/IDs/SQL. No emoji.
- **The mark** is a turtle (`branding/mark.svg`); ignore the stale amber `branding/lockup.svg`.
- **Components** in `components/` (Button, Card, Badge, Input, StatusPill, StepChip, AssertionRow); **full screens** in `ui_kits/web` and `ui_kits/mobile`.
- The product's soul is the **evidence chain** — show real numbers and expected-vs-actual, never just a green checkmark.
