import { defineConfig, ViteDevServer } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chatMiddleware } from "./chatMiddleware";

const __dirname = dirname(fileURLToPath(import.meta.url));

const apiMiddlewarePlugin = () => ({
  name: "chat-api-middleware",
  configureServer(server: ViteDevServer) {
    server.middlewares.use(chatMiddleware);
  },
});

export default defineConfig({
  plugins: [react(), tailwindcss(), apiMiddlewarePlugin()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "../src"),
    },
  },
});
