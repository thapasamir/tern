import { describe, expect, it } from "vitest";

import { ApiError } from "@/shared/api/errors";

import { describeAuthError } from "./messages";

const failure = (code: ApiError["code"], message = "raw") => new ApiError({ status: 400, code, message });

describe("describeAuthError", () => {
  it("does not say whether the email or the password was wrong", () => {
    expect(describeAuthError(failure("unauthorized"), "login")).toBe("That email and password don't match.");
  });

  it("points a duplicate sign-up at signing in", () => {
    expect(describeAuthError(failure("conflict"), "signup")).toContain("Sign in instead");
  });

  it("tells a throttled visitor to wait", () => {
    expect(describeAuthError(failure("rate_limited"), "login")).toContain("Wait a minute");
  });

  it("passes on the API's explanation of a bad request", () => {
    expect(describeAuthError(failure("invalid_request", "password is too short"), "signup")).toBe(
      "password is too short",
    );
  });

  it("falls back to a plain sentence for anything else", () => {
    expect(describeAuthError(failure("internal_error"), "login")).toBe("We couldn't sign you in. Try again.");
    expect(describeAuthError(failure("unknown"), "signup")).toBe("We couldn't create your account. Try again.");
  });
});
