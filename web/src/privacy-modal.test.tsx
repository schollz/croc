import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PrivacyModal } from "./privacy-modal";

describe("PrivacyModal", () => {
  afterEach(cleanup);

  it("requires a clear choice on a first visit", () => {
    const onChoose = vi.fn();
    render(
      <PrivacyModal
        currentChoice={null}
        onChoose={onChoose}
        onClose={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("dialog", { name: "Optional analytics" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Close privacy choices" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("dialog", { name: "Optional analytics" }),
    ).toHaveTextContent("Transfers work the same if you reject.");

    fireEvent.click(screen.getByRole("button", { name: "Reject analytics" }));
    expect(onChoose).toHaveBeenCalledWith("rejected");
  });

  it("can be closed after a saved choice and changed later", () => {
    const onChoose = vi.fn();
    const onClose = vi.fn();
    render(
      <PrivacyModal
        currentChoice="rejected"
        onChoose={onChoose}
        onClose={onClose}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "Close privacy choices" }),
    );
    expect(onClose).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByRole("button", { name: "Allow analytics" }));
    expect(onChoose).toHaveBeenCalledWith("accepted");
  });
});
