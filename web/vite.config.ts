import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, lazyPlugins } from "vite-plus";

// Core serves the built files (core/web, -tags embedweb); in development the API is proxied to a local Core.
const CORE = process.env.HAZELMINX_CORE_URL ?? "http://127.0.0.1:9800";

export default defineConfig({
  fmt: {
    printWidth: 120,
    ignorePatterns: ["dist/**", "src/api/schema.gen.ts"],
  },
  lint: {
    plugins: ["react", "typescript", "oxc"],
    ignorePatterns: ["dist/**", "src/api/schema.gen.ts"],
    rules: {
      "react/rules-of-hooks": "error",
      "react/only-export-components": ["warn", { allowConstantExport: true }],
      "vite-plus/prefer-vite-plus-imports": "error",
    },
    options: {
      typeAware: true,
      typeCheck: true,
    },
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
  },
  plugins: lazyPlugins(() => [tailwindcss(), react()]),
  server: {
    proxy: {
      "/management": { target: CORE, changeOrigin: false },
      "/healthz": { target: CORE },
    },
  },
  build: {
    outDir: "dist",
    assetsDir: "assets",
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
