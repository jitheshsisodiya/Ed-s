import type { Config } from 'tailwindcss';

export default {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#eef4ff',
          100: '#dfe8ff',
          200: '#c4d4ff',
          300: '#9db4ff',
          400: '#7089ff',
          500: '#4a5cf7',
          600: '#3a3fed',
          700: '#312fd0',
          800: '#2a29a8',
          900: '#282a85',
          950: '#181850',
        },
      },
      fontFamily: {
        sans: [
          'Inter',
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'Segoe UI',
          'Roboto',
          'sans-serif',
        ],
      },
    },
  },
  plugins: [],
} satisfies Config;
