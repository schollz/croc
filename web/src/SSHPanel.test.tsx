import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { WorkspaceSwitch } from "./App";
import SSHPanel from "./SSHPanel";
import type { SSHJoinCallbacks } from "./protocol/ssh";

const mocks = vi.hoisted(() => ({
  callbacks: undefined as SSHJoinCallbacks | undefined,
  onData: undefined as ((data: string) => void) | undefined,
  options: undefined as { disableStdin: boolean } | undefined,
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

    constructor(options: { callbacks?: SSHJoinCallbacks }) {
      mocks.callbacks = options.callbacks;
    }
  },
}));

function Harness() {
  const [mode, setMode] = useState<"files" | "ssh">("files");
  const [active, setActive] = useState(false);
  return (
    <>
      <WorkspaceSwitch mode={mode} disabled={active} onChange={setMode} />
      {mode === "files" ? (
        <div>File transfer workspace</div>
      ) : (
        <SSHPanel
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
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    render(<Harness />);
    expect(screen.getByText("File transfer workspace")).toBeVisible();
    fireEvent.click(screen.getByRole("tab", { name: "SSH" }));
    expect(await screen.findByText("Join a shared terminal")).toBeVisible();

    fireEvent.paste(screen.getByLabelText("SSH invitation"), {
      clipboardData: { getData: () => "acid-acorn-acre-acts-ahead-alien" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Join terminal" }));
    expect(screen.getByRole("tab", { name: "Files" })).toBeDisabled();

    act(() => {
      mocks.callbacks?.onRole?.("read-only");
      mocks.callbacks?.onConnected?.(true);
    });
    await waitFor(() => expect(mocks.options?.disableStdin).toBe(true));
    act(() => mocks.onData?.("whoami\r"));
    expect(mocks.sendInput).not.toHaveBeenCalled();
    expect(screen.getByText("Read-only")).toBeVisible();
  });
});
