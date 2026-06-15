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
        sans: ["ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "monospace"],
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
        // steel — steely-gray family for icons, dividers, and the new
        // background gradient.
        steel: {
          100: "#e5ecf2",
          200: "#c4cfdb",
          300: "#9aa7b6",
          400: "#7a8a9c",
          500: "#5d6c80",
          600: "#475467",
          700: "#33405a",
          800: "#252c35",
          900: "#1b2027",
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
