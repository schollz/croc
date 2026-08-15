import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  preloadWasm: vi.fn(),
  render: vi.fn(),
}));

vi.mock("./wasm/client", () => ({
  preloadWasm: mocks.preloadWasm,
}));
vi.mock("./App", () => ({ App: () => null }));
vi.mock("./Blog", () => ({ Blog: () => null }));
vi.mock("react-dom/client", () => ({
  default: {
    createRoot: () => ({ render: mocks.render }),
  },
}));

describe("web bootstrap", () => {
  beforeEach(() => {
    vi.resetModules();
    mocks.preloadWasm.mockClear();
    mocks.render.mockClear();
    document.body.innerHTML = '<div id="root"></div>';
  });

  it("preloads WASM on the transfer app", async () => {
    window.history.replaceState({}, "", "/");

    await import("./main");

    expect(mocks.preloadWasm).toHaveBeenCalledOnce();
    expect(mocks.render).toHaveBeenCalledOnce();
  });

  it("does not preload WASM on blog-only pages", async () => {
    window.history.replaceState({}, "", "/blog/pake-step-by-step");

    await import("./main");

    expect(mocks.preloadWasm).not.toHaveBeenCalled();
    expect(mocks.render).toHaveBeenCalledOnce();
  });
});
