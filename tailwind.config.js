/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./internal/web/**/*.templ", "./internal/web/**/*.go"],
  theme: {
    extend: {
      fontFamily: {
        sans: ["ui-sans-serif", "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "monospace"],
      },
      // brand — a calm, slightly desaturated navy. The single accent;
      // emerald stays reserved for the verified/passed state.
      colors: {
        brand: {
          50: "#eef2f8",
          100: "#d6e0ee",
          200: "#b0c4dd",
          300: "#84a0c6",
          400: "#5b7cab",
          500: "#3f5e8d",
          600: "#314a73",
          700: "#283c5d",
          800: "#21304a",
          900: "#18233a",
        },
      },
    },
  },
  plugins: [],
};
