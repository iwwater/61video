import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

const backendProxy = {
  "/api": "http://127.0.0.1:6192",
  "/p": "http://127.0.0.1:6192",
  "/admin/api": "http://127.0.0.1:6192",
};

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 6191,
    proxy: backendProxy,
  },
  preview: {
    host: "0.0.0.0",
    port: 6191,
    proxy: backendProxy,
  },
});
