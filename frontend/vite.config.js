import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { resolve } from 'path'

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: 'dist',
    rollupOptions: {
      input: {
        // Main Wails app window.
        main: resolve(__dirname, 'index.html'),
        // Standalone floating Quick Agent HUD (loaded by the native panel).
        hud: resolve(__dirname, 'hud.html'),
      },
    },
  },
})
