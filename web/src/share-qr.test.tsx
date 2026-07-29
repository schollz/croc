import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { makeDirectReceiveURL, ShareQRCode } from "./share-qr";

describe("receiver QR code", () => {
  it("builds a same-origin receive link with an encoded croc code", () => {
    expect(
      makeDirectReceiveURL(
        " 1234-word/word?& ",
        "https://send.example/path",
      ),
    ).toBe("https://send.example/?code=1234-word%2Fword%3F%26");
  });

  it("shows and hides the QR code on request", () => {
    const { rerender } = render(
      <ShareQRCode
        value="https://getcroc.com/?code=1234-test-code"
        description="Scan this code."
      />,
    );

    const toggle = screen.getByRole("button", { name: "Show QR code" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByRole("region", { name: "Receiver QR code" }),
    ).not.toBeInTheDocument();

    fireEvent.click(toggle);
    expect(
      screen.getByRole("region", { name: "Receiver QR code" }),
    ).toContainElement(screen.getByTitle("Scan to open the croc receive page"));
    expect(toggle).toHaveTextContent("Hide QR code");
    expect(toggle).toHaveAttribute("aria-expanded", "true");

    rerender(
      <ShareQRCode
        value="https://getcroc.com/?code="
        description="Scan this code."
        disabled
      />,
    );
    expect(toggle).toBeDisabled();
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.queryByRole("region", { name: "Receiver QR code" }),
    ).not.toBeInTheDocument();
  });
});
