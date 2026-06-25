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
  build: {
    target: "es2020",
    sourcemap: false,
    cssCodeSplit: true,
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router-dom"],
          artplayer: ["artplayer"],
          hlsjs: ["hls.js"],
          pdf: ["@wojtekmaj/react-pdf", "pdfjs-dist"],
        },
      },
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
