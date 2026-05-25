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
      // brand — warm amber/gold (the phoenix flame). The single accent;
      // emerald stays reserved for the verified/passed state. Cosmic-dark
      // backgrounds use the navy gradient in input.css.
      colors: {
        brand: {
          50:  "#fffbeb",
          100: "#fef3c7",
          200: "#fde68a",
          300: "#fcd34d",
          400: "#fbbf24",
          500: "#f59e0b",
          600: "#d97706",
          700: "#b45309",
          800: "#92400e",
          900: "#78350f",
        },
      },
    },
  },
  plugins: [],
};
