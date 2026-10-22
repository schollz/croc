import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  protocolEngine: vi.fn(),
  render: vi.fn(),
}));

vi.mock("./protocol/engine", () => ({
  protocolEngine: mocks.protocolEngine,
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
    mocks.protocolEngine.mockClear();
    mocks.render.mockClear();
    document.body.innerHTML = '<div id="root"></div>';
  });

  it("does not start the protocol engine before the user chooses a transfer", async () => {
    window.history.replaceState({}, "", "/");

    await import("./main");

    expect(mocks.protocolEngine).not.toHaveBeenCalled();
    expect(mocks.render).toHaveBeenCalledOnce();
  });

  it("does not start the protocol engine on blog-only pages", async () => {
    window.history.replaceState({}, "", "/blog/pake-step-by-step");

    await import("./main");

    expect(mocks.protocolEngine).not.toHaveBeenCalled();
    expect(mocks.render).toHaveBeenCalledOnce();
  });
});
