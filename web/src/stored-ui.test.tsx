import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  formatStoredExpiration,
  StoredExpirationControl,
  StoredModeSwitch,
  StoredShareCard,
  storedExpirationParts,
  storedExpirationSeconds,
  storedExpirationValueLimit,
} from "./stored-ui";

vi.mock("./share-qr", () => ({
  ShareQRCode: () => <div data-testid="share-qr" />,
}));

const upload = {
  share: {
    origin: "https://files.example.test",
    id: "AwMDAwMDAwMDAwMDAwMDAw",
    key: new Uint8Array(32),
  },
  uploadToken: "upload-token",
  expiresAt: "2026-08-12T00:00:00Z",
  browserURL:
    "https://files.example.test/s/AwMDAwMDAwMDAwMDAwMDAw#v1.example",
  cliToken: "croc-store-v1.example",
  downloads: 3,
};

describe("stored share copy feedback", () => {
  it("marks only the copied value as copied", async () => {
    const onCopy = vi.fn().mockResolvedValue(true);
    render(
      <StoredShareCard upload={upload} onCopy={onCopy} onRevoke={vi.fn()} />,
    );

    const copyButtons = screen.getAllByRole("button", { name: "Copy" });
    fireEvent.click(copyButtons[0]);

    await waitFor(() => expect(copyButtons[0]).toHaveTextContent("Copied"));
    expect(copyButtons[1]).toHaveTextContent("Copy");
    expect(onCopy).toHaveBeenCalledWith(upload.browserURL);
    expect(screen.getByText(/expires /)).toHaveTextContent(
      `expires ${new Date(upload.expiresAt).toLocaleString()}`,
    );
  });

  it("shows a copy failure", async () => {
    const onCopy = vi.fn().mockResolvedValue(false);
    render(
      <StoredShareCard upload={upload} onCopy={onCopy} onRevoke={vi.fn()} />,
    );

    const copyButtons = screen.getAllByRole("button", { name: "Copy" });
    fireEvent.click(copyButtons[1]);

    await waitFor(() =>
      expect(copyButtons[1]).toHaveTextContent("Copy failed"),
    );
  });
});

describe("stored expiration controls", () => {
  it("converts whole values and chooses a readable default unit", () => {
    expect(storedExpirationSeconds(90, "minutes")).toBe(5_400);
    expect(storedExpirationSeconds(2, "weeks")).toBe(1_209_600);
    expect(storedExpirationParts(86_400)).toEqual({ value: 1, unit: "days" });
    expect(storedExpirationParts(5_400)).toEqual({
      value: 90,
      unit: "minutes",
    });
    expect(formatStoredExpiration(1, "days")).toBe("1 day");
    expect(formatStoredExpiration(3, "days")).toBe("3 days");
  });

  it("constrains values and units to the runtime maximum", () => {
    const onChange = vi.fn();
    render(
      <StoredExpirationControl
        value={90}
        unit="minutes"
        maxExpiresSeconds={90 * 60}
        disabled={false}
        onChange={onChange}
      />,
    );

    expect(screen.getByLabelText("Storage lifetime")).toHaveAttribute(
      "max",
      "90",
    );
    expect(
      screen.getByRole("option", { name: "Days" }),
    ).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Storage lifetime unit"), {
      target: { value: "hours" },
    });
    expect(onChange).toHaveBeenCalledWith(1, "hours");
    expect(storedExpirationValueLimit("days", 90 * 60)).toBe(0);
  });

  it("uses the selected duration in stored-mode copy", () => {
    render(
      <StoredModeSwitch
        mode="stored"
        disabled={false}
        durationLabel="3 days"
        onChange={vi.fn()}
      />,
    );
    expect(
      screen.getByRole("button", { name: "Store for 3 days" }),
    ).toBeVisible();
  });
});
