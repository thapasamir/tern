import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Button } from "./button";

describe("Button", () => {
  it("is a real button", () => {
    render(<Button>Replay</Button>);
    expect(screen.getByRole("button", { name: "Replay" })).toBeEnabled();
  });

  it("can be disabled", () => {
    render(<Button disabled>Replay</Button>);
    expect(screen.getByRole("button", { name: "Replay" })).toBeDisabled();
  });

  it("can give its look to a link", () => {
    render(
      <Button asChild>
        <a href="/somewhere">Endpoints</a>
      </Button>,
    );
    expect(screen.getByRole("link", { name: "Endpoints" })).toHaveAttribute("href", "/somewhere");
  });
});
