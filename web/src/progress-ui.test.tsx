import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProgressBlock } from "./App";
import type { FileProgress } from "./protocol/types";

const started: FileProgress = {
  fileIndex: 0,
  fileCount: 1,
  fileName: "example.bin",
  fileBytes: 0,
  fileSize: 200,
  totalBytes: 0,
  totalSize: 200,
};

describe("completed transfer metrics", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("replaces ETA with elapsed time and keeps the final average rate", async () => {
    let now = 1_000;
    vi.spyOn(performance, "now").mockImplementation(() => now);
    const { rerender } = render(
      <ProgressBlock
        progress={started}
        status="Sending"
        completed={false}
        startedAtMilliseconds={1_000}
        sampledAtMilliseconds={1_000}
      />,
    );

    expect(screen.getByText("ETA")).toBeVisible();
    expect(screen.getByText("measuring…")).toBeVisible();
    expect(screen.getByText("calculating…")).toBeVisible();

    now = 9_000;
    rerender(
      <ProgressBlock
        progress={{
          ...started,
          fileBytes: 200,
          totalBytes: 200,
        }}
        status="Verifying"
        completed={false}
        startedAtMilliseconds={1_000}
        sampledAtMilliseconds={3_000}
      />,
    );
    await waitFor(() => expect(screen.getByText("100 B/s")).toBeVisible());
    expect(screen.getByText("ETA")).toBeVisible();
    expect(screen.getByText("0s")).toBeVisible();

    now = 12_000;
    rerender(
      <ProgressBlock
        progress={{
          ...started,
          fileBytes: 200,
          totalBytes: 200,
        }}
        status="Transfer complete"
        completed
        startedAtMilliseconds={1_000}
        sampledAtMilliseconds={3_000}
      />,
    );

    expect(screen.queryByText("ETA")).not.toBeInTheDocument();
    expect(screen.getByText("Elapsed")).toBeVisible();
    expect(screen.getByText("2s")).toBeVisible();
    expect(screen.getByText("100 B/s")).toBeVisible();
    expect(
      screen.getByLabelText("Average transfer rate and elapsed time"),
    ).toBeVisible();
  });

  it("does not show completed labels for an unsuccessful transfer", () => {
    render(
      <ProgressBlock
        progress={started}
        status="Transfer cancelled"
        completed={false}
      />,
    );

    expect(screen.getByText("ETA")).toBeVisible();
    expect(screen.queryByText("Elapsed")).not.toBeInTheDocument();
  });
});
