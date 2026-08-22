import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge, type Status } from "./status-badge";

describe("StatusBadge", () => {
  it.each<[Status, string]>([
    ["succeeded", "Succeeded"],
    ["failed", "Failed"],
    ["pending", "Pending"],
    ["active", "Active"],
    ["disabled", "Disabled"],
  ])("says %s in words, so colour never carries the meaning alone", (status, word) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(word)).toBeInTheDocument();
  });

  it("hides its shape from screen readers: the word is the label", () => {
    const { container } = render(<StatusBadge status="failed" />);
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  it("accepts a more specific label", () => {
    render(<StatusBadge status="failed" label="Failed after 8 attempts" />);
    expect(screen.getByText("Failed after 8 attempts")).toBeInTheDocument();
  });
});
