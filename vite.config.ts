import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

const backendProxy = {
  "/api": "http://127.0.0.1:6192",
  "/p": "http://127.0.0.1:6192",
  "/admin/api": "http://127.0.0.1:6192",
};

// Vite 8 内核是 Rolldown：manualChunks 只支持函数签名，不支持对象形式。
// 收到的是模块 id（绝对路径），用 includes() 判断归属哪个 chunk。
function manualChunks(id: string): string | undefined {
  if (!id.includes("node_modules")) return undefined;
  if (
    id.includes("/react/") ||
    id.includes("/react-dom/") ||
    id.includes("/react-router/") ||
    id.includes("/react-router-dom/")
  ) {
    return "react";
  }
  if (id.includes("/artplayer/")) return "artplayer";
  if (id.includes("/hls.js/")) return "hlsjs";
  if (
    id.includes("/@wojtekmaj/") ||
    id.includes("/react-pdf/") ||
    id.includes("/pdfjs-dist/")
  ) {
    return "pdf";
  }
  return undefined;
}

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
        manualChunks,
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
