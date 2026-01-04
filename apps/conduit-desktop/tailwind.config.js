/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: ['./src/renderer/**/*.{js,ts,jsx,tsx,html}'],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          '-apple-system',
          'BlinkMacSystemFont',
          'SF Pro Text',
          'SF Pro Display',
          'Helvetica Neue',
          'Arial',
          'sans-serif'
        ],
        mono: ['SF Mono', 'Menlo', 'Monaco', 'Courier New', 'monospace']
      },
      colors: {
        // macOS system colors
        macos: {
          blue: 'rgb(0, 122, 255)',
          green: 'rgb(52, 199, 89)',
          indigo: 'rgb(88, 86, 214)',
          orange: 'rgb(255, 149, 0)',
          pink: 'rgb(255, 45, 85)',
          purple: 'rgb(175, 82, 222)',
          red: 'rgb(255, 59, 48)',
          teal: 'rgb(90, 200, 250)',
          yellow: 'rgb(255, 204, 0)',
          // Background colors
          bg: {
            primary: 'rgb(246, 246, 246)',
            secondary: 'rgb(236, 236, 236)',
            tertiary: 'rgb(226, 226, 226)'
          },
          // Dark mode backgrounds
          'bg-dark': {
            primary: 'rgb(30, 30, 30)',
            secondary: 'rgb(44, 44, 46)',
            tertiary: 'rgb(58, 58, 60)'
          },
          // Text colors
          text: {
            primary: 'rgb(0, 0, 0)',
            secondary: 'rgba(0, 0, 0, 0.55)',
            tertiary: 'rgba(0, 0, 0, 0.25)'
          },
          'text-dark': {
            primary: 'rgb(255, 255, 255)',
            secondary: 'rgba(255, 255, 255, 0.55)',
            tertiary: 'rgba(255, 255, 255, 0.25)'
          },
          // Separator
          separator: 'rgba(0, 0, 0, 0.1)',
          'separator-dark': 'rgba(255, 255, 255, 0.1)'
        }
      },
      borderRadius: {
        lg: '10px',
        md: '8px',
        sm: '6px',
        xs: '4px'
      },
      boxShadow: {
        macos:
          '0 0 0 0.5px rgba(0, 0, 0, 0.12), 0 2px 8px rgba(0, 0, 0, 0.08), 0 1px 2px rgba(0, 0, 0, 0.04)',
        'macos-lg':
          '0 0 0 0.5px rgba(0, 0, 0, 0.12), 0 8px 32px rgba(0, 0, 0, 0.12), 0 4px 12px rgba(0, 0, 0, 0.08)'
      },
      backdropBlur: {
        macos: '20px'
      },
      keyframes: {
        'fade-in': {
          from: { opacity: '0' },
          to: { opacity: '1' }
        },
        'slide-in-right': {
          from: { transform: 'translateX(10px)', opacity: '0' },
          to: { transform: 'translateX(0)', opacity: '1' }
        },
        'slide-in-bottom': {
          from: { transform: 'translateY(10px)', opacity: '0' },
          to: { transform: 'translateY(0)', opacity: '1' }
        }
      },
      animation: {
        'fade-in': 'fade-in 150ms ease-out',
        'slide-in-right': 'slide-in-right 150ms ease-out',
        'slide-in-bottom': 'slide-in-bottom 150ms ease-out'
      }
    }
  },
  plugins: [require('tailwindcss-animate')]
}
