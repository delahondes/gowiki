import { defineConfig } from "vite"

export default defineConfig({
  test: {
    environment: "jsdom",
    globals: true,
    include: ["test/**/*.test.ts"],
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        ws: true,
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
