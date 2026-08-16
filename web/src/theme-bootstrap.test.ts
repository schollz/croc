import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

const indexHTML = readFileSync(
  resolve(process.cwd(), "index.html"),
  "utf8",
);
const bootstrapMatch = indexHTML.match(
  /<script data-croc-theme-bootstrap>([\s\S]*?)<\/script>/,
);

if (!bootstrapMatch) throw new Error("Theme bootstrap script is missing");

const themeBootstrap = bootstrapMatch[1];
const storedValues = new Map<string, string>();

function emulateSystemTheme(theme: "dark" | "light") {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn((query: string) => ({
      matches:
        query === "(prefers-color-scheme: light)" && theme === "light",
    })),
  });
}

function runThemeBootstrap() {
  window.eval(themeBootstrap);
}

describe("theme bootstrap", () => {
  beforeEach(() => {
    storedValues.clear();
    vi.stubGlobal("localStorage", {
      clear: () => storedValues.clear(),
      getItem: (key: string) => storedValues.get(key) ?? null,
      setItem: (key: string, value: string) => storedValues.set(key, value),
    });
    document.documentElement.removeAttribute("data-theme");
    document.documentElement.removeAttribute("style");
    document.head.innerHTML = '<meta name="theme-color" content="#000000" />';
  });

  it("applies the saved theme before the app module loads", () => {
    localStorage.setItem("croc-web-theme", "light");
    emulateSystemTheme("dark");

    runThemeBootstrap();

    expect(document.documentElement.dataset.theme).toBe("light");
    expect(document.documentElement.style.colorScheme).toBe("light");
    expect(document.querySelector('meta[name="theme-color"]')).toHaveAttribute(
      "content",
      "#f2f1ec",
    );
    expect(indexHTML.indexOf("data-croc-theme-bootstrap")).toBeLessThan(
      indexHTML.indexOf('type="module"'),
    );
  });

  it("uses the system theme when there is no saved preference", () => {
    emulateSystemTheme("light");

    runThemeBootstrap();

    expect(document.documentElement.dataset.theme).toBe("light");
  });
});
