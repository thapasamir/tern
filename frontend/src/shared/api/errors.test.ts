import { describe, expect, it } from "vitest";

import { ApiError, toApiError } from "./errors";

describe("toApiError", () => {
  it("reads the error shape the API reports", () => {
    const err = toApiError(409, {
      error: { code: "conflict", message: "an account with this email already exists", request_id: "req-1" },
    });

    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({
      status: 409,
      code: "conflict",
      message: "an account with this email already exists",
      requestId: "req-1",
    });
  });

  it("copes without a request id", () => {
    const err = toApiError(401, { error: { code: "unauthorized", message: "invalid or missing API key" } });
    expect(err.requestId).toBeUndefined();
  });

  it.each([
    ["an HTML page from a gateway", "<html>Bad gateway</html>"],
    ["nothing at all", undefined],
    ["null", null],
    ["an error without a message", { error: { code: "internal_error" } }],
    ["an error that is not an object", { error: "boom" }],
  ])("still gives a usable error for %s", (_, body) => {
    const err = toApiError(502, body);
    expect(err.code).toBe("unknown");
    expect(err.status).toBe(502);
    expect(err.message).toContain("502");
  });

  it("knows a not-found answer", () => {
    expect(toApiError(404, { error: { code: "not_found", message: "no such delivery" } }).isNotFound).toBe(true);
    expect(toApiError(500, { error: { code: "internal_error", message: "x" } }).isNotFound).toBe(false);
  });
});
