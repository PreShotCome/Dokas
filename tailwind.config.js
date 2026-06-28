/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/**/*.templ", "./internal/web/**/*.go"],
  // Dark-only brand: <html class="dark"> is hard-set in layout.templ so
  // every existing `dark:` modifier fires unconditionally. Keep the
  // modifiers in templates — they're the dark values, not alternates.
  darkMode: "class",
  theme: {
    extend: {
      fontFamily: {
        // Geist (sans) + JetBrains Mono — loaded from Google Fonts in
        // layout.templ. ui-sans-serif/ui-monospace are the system fallbacks
        // for the brief window before the webfont swaps in.
        sans: ["Geist", "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["\"JetBrains Mono\"", "ui-monospace", "SFMono-Regular", "monospace"],
      },
      // brand — baby royal blue. Primary tier: buttons, links, the
      // turtle's shell. Cool, calm, "your data is held".
      colors: {
        brand: {
          50:  "#eef4ff",
          100: "#dbe7ff",
          200: "#b8cfff",
          300: "#8cb1f7",
          400: "#6e9bf0",
          500: "#5b8def",
          600: "#4f7be0",
          700: "#3f63c4",
          800: "#33509f",
          900: "#243a78",
        },
        // gold (legacy class name) — now pink. Accent for the "MOST
        // POPULAR" badge and the central scute of the turtle. Kept as
        // `gold-*` so every existing template class still works.
        gold: {
          50:  "#fdf2f8",
          100: "#fce7f3",
          200: "#fbcfe8",
          300: "#f9a8d4",
          400: "#f48fb1",
          500: "#ec4899",
          600: "#db2777",
          700: "#be185d",
        },
        // steel — full charcoal ramp. 800/900 are the page; #2e3640 (between
        // 700 and 800) is the canonical card surface and is wired into the
        // .card component in input.css.
        steel: {
          50:  "#f4f7fa",
          100: "#e5ecf2",
          200: "#c4cfdb",
          300: "#9aa7b6",
          400: "#7a8a9c",
          500: "#5d6c80",
          600: "#475467",
          700: "#33405a",
          800: "#252c35",
          900: "#1b2027",
          950: "#11151a",
        },
        // teal — sea-teal for "passed / up / verified". Cooler than emerald,
        // reinforces the beach-meets-audit-room mood; the success color for
        // every positive verdict (drill passed, heartbeat up, signature ok).
        teal: {
          50:  "#e6f7f1",
          100: "#c2ece0",
          200: "#8fdcc6",
          300: "#5fcbac",
          400: "#56c596",
          500: "#2ba888",
          600: "#1f8a70",
          700: "#186b58",
        },
        // danger — pink-red, harmonises with the pink accent rather than
        // crashing against it. Used for failed/down verdicts.
        danger: {
          DEFAULT: "#ff6b81",
          strong:  "#e23d56",
        },
        // lapis — accent blue (kept for any existing references).
        lapis: {
          100: "#dbeafe",
          300: "#93c5fd",
          500: "#3b82f6",
          700: "#1d4ed8",
          800: "#1e40af",
          900: "#1e3a8a",
        },
      },
    },
  },
  plugins: [],
};
