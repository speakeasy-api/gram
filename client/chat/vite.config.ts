import path from "node:path";
import fs from "node:fs";
import process from "node:process";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

let key: Buffer | undefined;
let cert: Buffer | undefined;

if (process.env["GRAM_SSL_KEY_FILE"] && process.env["GRAM_SSL_CERT_FILE"]) {
  key = fs.readFileSync(process.env["GRAM_SSL_KEY_FILE"]);
  cert = fs.readFileSync(process.env["GRAM_SSL_CERT_FILE"]);
}

export default defineConfig(({ command }) => {
  const isDev = command === "serve";

  const serverUrl = process.env["GRAM_SERVER_URL"];
  if (isDev && !serverUrl) {
    throw new Error("GRAM_SERVER_URL must be set in development");
  }

  return {
    define: {
      __GRAM_SERVER_URL__: JSON.stringify(serverUrl),
    },
    build: {
      target: "es2022",
      sourcemap: true,
      rollupOptions: {
        input: {
          main: path.resolve(__dirname, "index.html"),
        },
        output: {
          manualChunks: {
            "lucide-react": ["lucide-react"],
            externals: ["react", "react-dom", "react-router"],
          },
        },
      },
    },
    esbuild: {
      target: "es2022",
    },
    server: {
      host: true,
      allowedHosts: ["localhost", "127.0.0.1"],
      https: key && cert ? { key, cert } : void 0,
      proxy: serverUrl
        ? {
            "/rpc": serverUrl,
            "/chat": serverUrl,
            "/mcp": serverUrl,
            "/oauth": serverUrl,
            "/oauth-external": serverUrl,
            "/.well-known": serverUrl,
          }
        : undefined,
    },
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
        react: path.resolve(__dirname, "node_modules/react"),
        "react-dom": path.resolve(__dirname, "node_modules/react-dom"),
        "@assistant-ui/react": path.resolve(
          __dirname,
          "node_modules/@assistant-ui/react",
        ),
      },
    },
  };
});
