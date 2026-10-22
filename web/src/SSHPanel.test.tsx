import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

beforeEach(() => {
  vi.clearAllMocks();
  mocks.callbacks = undefined;
  mocks.onData = undefined;
  mocks.options = undefined;
  mocks.joinCode = undefined;
  window.history.replaceState({}, "", "/");
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  cleanup();
  delete window.umami;
  vi.unstubAllGlobals();
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
  it("allows manual invitation edits while hiding and revealing the code", () => {
    window.history.replaceState({}, "", "/#ssh");
    render(<Harness />);

    const input = screen.getByLabelText("SSH invitation");
    const join = screen.getByRole("button", { name: "Join terminal" });
    expect(input).toBeEnabled();
    expect(input).not.toHaveAttribute("readonly");
    expect(input).toHaveAttribute("type", "password");
    expect(join).toBeDisabled();

    fireEvent.change(input, { target: { value: "   " } });
    expect(join).toBeDisabled();
    fireEvent.click(join);
    expect(mocks.joinCode).toBeUndefined();

    const invitation = "acid-acorn-acre-acts-ahead-alien";
    fireEvent.change(input, { target: { value: `${invitation}-typo` } });
    expect(join).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Show SSH invitation" }));
    expect(input).toHaveAttribute("type", "text");
    expect(input).toHaveValue(`${invitation}-typo`);

    fireEvent.change(input, { target: { value: invitation } });
    fireEvent.click(screen.getByRole("button", { name: "Hide SSH invitation" }));
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveValue(invitation);
    expect(mocks.joinCode).toBeUndefined();

    fireEvent.change(input, { target: { value: "" } });
    expect(input).toHaveValue("");
    expect(join).toBeDisabled();
  });

  it("joins with the current trimmed invitation and disables edits while connecting", () => {
    window.history.replaceState({}, "", "/#ssh");
    render(<Harness />);

    const input = screen.getByLabelText("SSH invitation");
    const join = screen.getByRole("button", { name: "Join terminal" });
    const invitation = "acid-acorn-acre-acts-ahead-alien";
    fireEvent.change(input, { target: { value: `${invitation}-typo` } });
    fireEvent.change(input, { target: { value: `  ${invitation}  ` } });
    expect(input).toHaveValue(`  ${invitation}  `);
    fireEvent.click(join);

    expect(mocks.joinCode).toBe(invitation);
    expect(input).toBeDisabled();
    expect(join).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Show SSH invitation" }),
    ).toBeDisabled();
    expect(screen.getByRole("tab", { name: "Files" })).toBeDisabled();
  });

  it("switches from files and suppresses local input for read-only access", async () => {
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
    expect(screen.getByLabelText("SSH invitation")).toHaveValue(
      "acid-acorn-acre-acts-ahead-alien",
    );
    fireEvent.click(screen.getByRole("button", { name: "Join terminal" }));
    expect(mocks.joinCode).toBe("acid-acorn-acre-acts-ahead-alien");
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
