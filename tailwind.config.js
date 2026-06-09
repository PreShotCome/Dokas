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
      // brand — deep ceremonial red (the Selket scorpion's red). The
      // primary tier. Emerald stays reserved for the verified/passed
      // state. Cosmic-dark backgrounds use the navy gradient in input.css
      // (the "touches of blue" in the Selket palette).
      colors: {
        brand: {
          50:  "#fef2f2",
          100: "#fee2e2",
          200: "#fecaca",
          300: "#fca5a5",
          400: "#f87171",
          500: "#ef4444",
          600: "#dc2626",
          700: "#b91c1c",
          800: "#991b1b",
          900: "#7f1d1d",
        },
        // gold — Egyptian royal gold. The secondary tier, paired with
        // brand-red for flame gradients (deep red → gold) and used for
        // ornamental highlights, the "popular" tier badge, and the
        // signed-evidence sigil.
        gold: {
          50:  "#fefce8",
          100: "#fef9c3",
          200: "#fef08a",
          300: "#fde047",
          400: "#facc15",
          500: "#eab308",
          600: "#ca8a04",
          700: "#a16207",
        },
        // lapis — accent blue (the canopic-jar / Nile touch). Used
        // sparingly for icons, info chips, and the cosmic-dark
        // background's blue undertone.
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
