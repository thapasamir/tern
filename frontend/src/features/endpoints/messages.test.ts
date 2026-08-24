import { describe, expect, it } from "vitest";

import { ApiError } from "@/shared/api/errors";

import { describeEndpointError } from "./messages";

const failure = (code: ApiError["code"], message = "raw") => new ApiError({ status: 400, code, message });

describe("describeEndpointError", () => {
  it("shows the API's reason for a rejected URL", () => {
    expect(describeEndpointError(failure("invalid_request", "url resolves to a private address"))).toBe(
      "url resolves to a private address",
    );
  });

  it("says when the endpoint is gone", () => {
    expect(describeEndpointError(failure("not_found"))).toBe("That endpoint no longer exists.");
  });

  it("does not leak internals for an unexpected failure", () => {
    expect(describeEndpointError(failure("internal_error", "pq: deadlock"))).toBe(
      "Something went wrong on our side. Try again.",
    );
  });
});
