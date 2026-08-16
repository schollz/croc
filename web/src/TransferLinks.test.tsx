import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TransferLinks } from "./TransferLinks";

describe("TransferLinks", () => {
  it("offers the same transfer resources on every page", () => {
    render(<TransferLinks />);

    const navigation = screen.getByRole("navigation", {
      name: "More ways to transfer with croc",
    });
    const links = within(navigation).getAllByRole("link");

    expect(links).toHaveLength(5);
    expect(links.map((link) => link.getAttribute("href"))).toEqual([
      "/#send-panel",
      "/#receive",
      "/#cli-download",
      "https://infinitedigits.co/croc/",
      "https://github.com/schollz/croc",
    ]);
  });
});
