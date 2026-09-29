import { defineConfig } from 'vite'

// Static-HTML-first, same as onagent's own apps/landing: Vite here is just
// a dev server + bundler for plain HTML/CSS/JS, not a framework app.
// Builds to the default dist/ (gitignored).
export default defineConfig({})
