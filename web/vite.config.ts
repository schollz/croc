import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";

function inlineCss(): Plugin {
  return {
    name: "inline-css",
    enforce: "post",
    generateBundle(_options, bundle) {
      const stylesheetLink =
        /<link\b(?=[^>]*\brel=["']stylesheet["'])(?=[^>]*\bhref=["']([^"']+)["'])[^>]*>/gi;
      const inlinedCss = new Set<string>();

      for (const output of Object.values(bundle)) {
        if (output.type !== "asset" || !output.fileName.endsWith(".html")) {
          continue;
        }

        const html = String(output.source).replace(
          stylesheetLink,
          (link, href: string) => {
            const fileName = new URL(href, "https://vite.invalid/").pathname
              .replace(/^\//, "");
            const stylesheet = bundle[fileName];

            if (
              stylesheet?.type !== "asset" ||
              !stylesheet.fileName.endsWith(".css")
            ) {
              return link;
            }

            inlinedCss.add(fileName);
            const css = String(stylesheet.source).replace(
              /<\/style/gi,
              "<\\/style",
            );
            return `<style data-croc-inline-css>${css}</style>`;
          },
        );

        output.source = html;
      }

      for (const fileName of inlinedCss) {
        delete bundle[fileName];
      }
    },
  };
}

const gatewayProxy =
  process.env.CROC_GATEWAY_PROXY ?? "ws://127.0.0.1:9014";

export default defineConfig({
  plugins: [react(), inlineCss()],
  server: {
    fs: {
      allow: [".."],
    },
    host: "127.0.0.1",
    proxy: {
      "/ws": {
        target: gatewayProxy,
        ws: true,
      },
      "/healthz": {
        target: gatewayProxy.replace(/^ws/, "http"),
      },
      "/api": {
        target: gatewayProxy.replace(/^ws/, "http"),
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    exclude: [...configDefaults.exclude, "e2e/**"],
  },
});
