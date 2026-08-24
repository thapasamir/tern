import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ConfirmAction } from "./confirm-action";

function setup() {
  const action = vi.fn();
  render(
    <ConfirmAction
      action={action}
      label="Delete endpoint"
      question="Delete this endpoint?"
      confirmLabel="Delete for good"
      pendingLabel="Deleting…"
    />,
  );
  return { action, user: userEvent.setup() };
}

describe("ConfirmAction", () => {
  it("only asks on the first click", async () => {
    const { action, user } = setup();

    await user.click(screen.getByRole("button", { name: "Delete endpoint" }));

    expect(screen.getByRole("alert")).toHaveTextContent("Delete this endpoint?");
    expect(screen.getByRole("button", { name: "Delete for good" })).toBeInTheDocument();
    expect(action).not.toHaveBeenCalled();
  });

  it("runs the action only from the confirm button", async () => {
    const { action, user } = setup();

    await user.click(screen.getByRole("button", { name: "Delete endpoint" }));
    await user.click(screen.getByRole("button", { name: "Delete for good" }));

    expect(action).toHaveBeenCalledTimes(1);
  });

  it("goes back to the plain button on cancel, without running anything", async () => {
    const { action, user } = setup();

    await user.click(screen.getByRole("button", { name: "Delete endpoint" }));
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    expect(screen.getByRole("button", { name: "Delete endpoint" })).toBeInTheDocument();
    expect(action).not.toHaveBeenCalled();
  });
});
