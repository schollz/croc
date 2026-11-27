import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TransferCallbacks } from "./protocol/types";

const mocks = vi.hoisted(() => ({
  sendFiles: vi.fn(),
  prepareFiles: vi.fn(),
  prepareText: vi.fn(),
  generateCodeForRelay: vi.fn(),
  clearRelay: vi.fn(),
}));

vi.mock("./protocol/client", () => ({
  sendFiles: mocks.sendFiles,
  prepareFiles: mocks.prepareFiles,
  prepareText: mocks.prepareText,
  receiveFiles: vi.fn(),
}));
vi.mock("./protocol/hash", () => ({
  FileHashCache: class {
    prime() {}
    retain() {}
    clear() {}
    hash() { return Promise.resolve(new Uint8Array(8)); }
  },
}));
vi.mock("./public-relay", () => ({
  RelayPreloader: class {
    preload() { return Promise.resolve(0); }
    select() { return Promise.resolve(0); }
    clear = mocks.clearRelay;
  },
  generateCodeForRelay: mocks.generateCodeForRelay,
  clearBestRelayOnConnectionError: () => false,
}));
vi.mock("./share-qr", async (original) => ({
  ...await original<typeof import("./share-qr")>(),
  ShareQRCode: ({ value }: { value: string }) => <output aria-label="QR value">{value}</output>,
}));

import { App } from "./App";

type Session = {
  secret: string;
  signal: AbortSignal;
  callbacks: TransferCallbacks;
  resolve(): void;
  reject(error: Error): void;
};
const sessions: Session[] = [];

beforeEach(() => {
  window.history.replaceState({}, "", "/#send-panel");
  localStorage.clear();
  sessionStorage.clear();
  vi.clearAllMocks();
  sessions.length = 0;
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline test")));
  mocks.prepareFiles.mockImplementation(async (files) => files);
  mocks.prepareText.mockImplementation(async (text) => [text]);
  mocks.generateCodeForRelay.mockImplementation(async (_index, _count, generate) => generate());
  mocks.sendFiles.mockImplementation((options) => new Promise<void>((resolve, reject) => {
    sessions.push({ ...options, resolve, reject });
    options.callbacks.onStatus("Waiting for recipient…");
    options.callbacks.onConnectionPhase("waiting");
  }));
});

afterEach(async () => {
  cleanup();
  await act(async () => {
    for (const session of sessions) session.resolve();
  });
  vi.unstubAllGlobals();
});

async function start() {
  const view = render(<App />);
  fireEvent.change(view.container.querySelector('input[type="file"]')!, {
    target: { files: [new File(["hello"], "hello.txt")] },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send file" }));
  await waitFor(() => expect(sessions).toHaveLength(1));
  return screen.getByRole("checkbox", { name: "Use letters instead" });
}

describe("live send code format", () => {
  it("serializes replacements and ignores stale callbacks and completion", async () => {
    const checkbox = await start();
    const original = sessions[0];
    expect(screen.getByLabelText("Croc code")).toHaveAttribute("data-format", "words");
    fireEvent.click(checkbox);
    expect(original.signal.aborted).toBe(true);
    expect(checkbox).toBeDisabled();
    expect(screen.getByLabelText("Croc code")).toBeEmptyDOMElement();
    expect(sessions).toHaveLength(1);

    act(() => {
      original.callbacks.onStatus?.("stale status");
      original.callbacks.onConnectionPhase?.("connected");
      original.callbacks.onFileComplete?.("stale file");
    });
    expect(screen.queryByText("stale status")).not.toBeInTheDocument();
    await act(async () => original.resolve());
    await waitFor(() => expect(sessions).toHaveLength(2));
    const letters = sessions[1];
    expect(letters.secret).toMatch(/^[a-z]{8}-[a-z]{8}-[a-z]{8}$/);
    expect(screen.getByLabelText("Croc code")).toHaveTextContent(letters.secret);
    expect((screen.getByLabelText("Browser link") as HTMLInputElement).value).toContain(letters.secret);
    expect(screen.getByLabelText("QR value")).toHaveTextContent(letters.secret);
    expect(checkbox).toBeEnabled();
    expect(screen.queryByText("All files arrived safely")).not.toBeInTheDocument();
    expect(mocks.prepareFiles.mock.calls[0][0]).toEqual(mocks.prepareFiles.mock.calls[1][0]);

    fireEvent.click(checkbox);
    expect(letters.signal.aborted).toBe(true);
    await act(async () => letters.reject(new Error("old session closed")));
    await waitFor(() => expect(sessions).toHaveLength(3));
    expect(screen.getByLabelText("Croc code")).toHaveAttribute("data-format", "words");
    expect(screen.queryByText("old session closed")).not.toBeInTheDocument();
    expect(checkbox).not.toBeChecked();
  });

  it("locks switching synchronously when a recipient connects", async () => {
    const checkbox = await start();
    act(() => {
      sessions[0].callbacks.onConnectionPhase?.("connected");
      fireEvent.click(checkbox);
    });
    expect(checkbox).toBeDisabled();
    expect(sessions[0].signal.aborted).toBe(false);
    expect(sessions).toHaveLength(1);
    await act(async () => sessions[0].resolve());
    expect(screen.getByText("All files arrived safely")).toBeVisible();
    expect(checkbox).toBeDisabled();
  });

  it.each(["Copy code", "Copy browser link"])("ignores late %s feedback after a restart", async (button) => {
    let finishCopy: () => void = () => {};
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: () => new Promise<void>((resolve) => { finishCopy = resolve; }) },
    });
    const checkbox = await start();
    fireEvent.click(screen.getByRole("button", { name: button }));
    fireEvent.click(checkbox);
    await act(async () => sessions[0].resolve());
    await waitFor(() => expect(sessions).toHaveLength(2));
    await act(async () => finishCopy());
    expect(screen.getByLabelText("Croc code")).not.toHaveClass("copied");
    expect(screen.queryByRole("button", { name: "Code copied" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Browser link copied" })).not.toBeInTheDocument();
  });

  it("retains letters for subsequent sends and recovers from regeneration errors", async () => {
    const checkbox = await start();
    mocks.generateCodeForRelay.mockRejectedValueOnce(new Error("random generation failed"));
    fireEvent.click(checkbox);
    await act(async () => sessions[0].reject(new Error("aborted")));
    await screen.findByText("random generation failed");
    expect(checkbox).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Send file" }));
    await waitFor(() => expect(sessions).toHaveLength(2));
    expect(sessions[1].secret).toMatch(/^[a-z]{8}-[a-z]{8}-[a-z]{8}$/);
    await act(async () => sessions[1].resolve());
    fireEvent.click(screen.getByRole("button", { name: "Send file" }));
    await waitFor(() => expect(sessions).toHaveLength(3));
    expect(sessions[2].secret).toMatch(/^[a-z]{8}-[a-z]{8}-[a-z]{8}$/);
  });
});
