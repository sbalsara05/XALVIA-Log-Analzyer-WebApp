import path from "path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],

  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },

  server: {
    port: 5173,
    host: "0.0.0.0", // listen on all interfaces so Docker can expose the port

    proxy: {
      "/api": {
        target:
          "https://xaliva-gateway-494895754428.us-east4.run.app",
        changeOrigin: true,
        secure: true,
      },
    },
  },

  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});