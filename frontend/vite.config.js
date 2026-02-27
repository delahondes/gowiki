import { defineConfig } from "vite"

export default defineConfig({
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      "^/media/": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
      // Proxy user-owned attachment-like paths, but keep app-owned UI assets under /icons local.
      "^/(?!icons/).+\\.(png|jpe?g|gif|svg|webp|pdf|zip|gz|tar|tgz|md)(\\?.*)?$": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
})
