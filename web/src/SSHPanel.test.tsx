import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useWorkspaceMode, WorkspaceSwitch } from "./App";
import SSHPanel from "./SSHPanel";
import type { SSHJoinCallbacks } from "./protocol/ssh";

const mocks = vi.hoisted(() => ({
  callbacks: undefined as SSHJoinCallbacks | undefined,
  onData: undefined as ((data: string) => void) | undefined,
  options: undefined as { disableStdin: boolean } | undefined,
  joinCode: undefined as string | undefined,
  sendInput: vi.fn(),
  disconnect: vi.fn(),
}));

vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    options: { disableStdin: boolean };
    cols = 80;
    rows = 24;

    constructor(options: { disableStdin: boolean }) {
      this.options = options;
      mocks.options = this.options;
    }

    loadAddon() {}
    open() {}
    focus() {}
    reset() {}
    dispose() {}
    write(_data: Uint8Array, callback?: () => void) {
      callback?.();
    }
    onData(callback: (data: string) => void) {
      mocks.onData = callback;
      return { dispose() {} };
    }
    attachCustomKeyEventHandler() {}
  },
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class {
    fit() {}
  },
}));

vi.mock("./protocol/ssh", () => ({
  SSHJoinSession: class {
    done = new Promise<void>(() => {});
    sendInput = mocks.sendInput;
    disconnect = mocks.disconnect;
    resize() {}

    constructor(options: { code: string; callbacks?: SSHJoinCallbacks }) {
      mocks.joinCode = options.code;
      mocks.callbacks = options.callbacks;
    }
  },
}));

afterEach(() => {
  cleanup();
  delete window.umami;
});

function Harness() {
  const [mode, setMode, initialCode] = useWorkspaceMode();
  const [active, setActive] = useState(false);
  return (
    <>
      <WorkspaceSwitch mode={mode} disabled={active} onChange={setMode} />
      {mode === "files" ? (
        <div>File transfer workspace</div>
      ) : (
        <SSHPanel
          key={initialCode}
          initialCode={initialCode}
          settings={{
            gatewayURL: "/ws",
            relayAddresses: ["relay.example:9009"],
            relayPassword: "pass123",
            storeAPI: "/api/v1/store",
          }}
          theme="dark"
          onActiveChange={setActive}
        />
      )}
    </>
  );
}

describe("SSH homepage mode", () => {
  it("switches from files and suppresses local input for read-only access", async () => {
    window.history.replaceState({}, "", "/");
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    const track = vi.fn();
    window.umami = { track };
    render(<Harness />);
    expect(screen.getByText("File transfer workspace")).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "SSH" }));
    expect(window.location.hash).toBe("#ssh");
    expect(await screen.findByText("Join a shared terminal")).toBeVisible();

    fireEvent.paste(screen.getByLabelText("SSH invitation"), {
      clipboardData: { getData: () => "acid-acorn-acre-acts-ahead-alien" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Join terminal" }));
    expect(screen.getByRole("tab", { name: "Files" })).toBeDisabled();

    act(() => {
      mocks.callbacks?.onRole?.("read-only");
      mocks.callbacks?.onConnected?.(true);
      mocks.callbacks?.onConnected?.(false);
      mocks.callbacks?.onConnected?.(true);
    });
    await waitFor(() => expect(mocks.options?.disableStdin).toBe(true));
    expect(track).toHaveBeenCalledOnce();
    act(() => mocks.onData?.("whoami\r"));
    expect(mocks.sendInput).not.toHaveBeenCalled();
    expect(screen.getByText("Read-only")).toBeVisible();
  });

  it("opens shared SSH URLs directly and clears its hash when returning to files", () => {
    window.history.replaceState({}, "", "/#ssh");
    render(<Harness />);

    expect(screen.getByText("Join a shared terminal")).toBeVisible();
    expect(screen.getByRole("tab", { name: "SSH" })).toHaveAttribute(
      "aria-selected",
      "true",
    );

    fireEvent.click(screen.getByRole("tab", { name: "Files" }));

    expect(window.location.hash).toBe("");
    expect(screen.getByText("File transfer workspace")).toBeVisible();
  });

  it("auto-joins invitation links and removes the secret from the address bar", async () => {
    window.history.replaceState(
      {},
      "",
      "/#ssh?code=acid-acorn-acre-acts-ahead-alien",
    );
    render(<Harness />);

    await waitFor(() => {
      expect(mocks.joinCode).toBe("acid-acorn-acre-acts-ahead-alien");
      expect(screen.getByRole("tab", { name: "Files" })).toBeDisabled();
    });
    expect(window.location.hash).toBe("#ssh");
  });

  it("follows browser navigation between file and SSH URLs", () => {
    window.history.replaceState({}, "", "/");
    render(<Harness />);

    act(() => {
      window.history.replaceState({}, "", "/#ssh");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });

    expect(screen.getByText("Join a shared terminal")).toBeVisible();
  });
});
