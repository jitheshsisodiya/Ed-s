import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// main.go embeds frontend/dist with go:embed, which fails outright when the
// directory does not exist — so a fresh clone needs a committed placeholder
// inside it. Vite empties the directory on every build and takes the
// placeholder with it, which is how `go build` ends up broken on a clone
// nobody has touched. Putting it back after the bundle closes stops the two
// tools from undoing each other.
function keepEmbedDirTracked() {
  return {
    name: 'nexusvpn-keep-embed-dir-tracked',
    closeBundle() {
      writeFileSync(resolve(__dirname, 'dist/.gitkeep'), '')
    },
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss(), keepEmbedDirTracked()],
})
