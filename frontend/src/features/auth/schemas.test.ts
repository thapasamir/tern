import { describe, expect, it } from "vitest";

import { loginSchema, signupSchema } from "./schemas";

describe("loginSchema", () => {
  it("trims the email and keeps the password exactly as typed", () => {
    expect(loginSchema.parse({ email: "  a@b.co ", password: " pass " })).toEqual({
      email: "a@b.co",
      password: " pass ",
    });
  });

  it("explains each missing field", () => {
    const result = loginSchema.safeParse({ email: "", password: "" });
    expect(result.success).toBe(false);
    expect(result.error?.issues.map((i) => i.message)).toEqual(["Enter your email.", "Enter your password."]);
  });

  it("rejects something that is not an email", () => {
    expect(loginSchema.safeParse({ email: "nope", password: "x" }).success).toBe(false);
  });
});

describe("signupSchema", () => {
  const valid = { tenant_name: "Acme", email: "a@b.co", password: "long enough" };

  it("accepts a complete form", () => {
    expect(signupSchema.parse(valid)).toEqual(valid);
  });

  it("requires a password of 8 to 128 characters, like the API", () => {
    expect(signupSchema.safeParse({ ...valid, password: "short" }).success).toBe(false);
    expect(signupSchema.safeParse({ ...valid, password: "x".repeat(129) }).success).toBe(false);
    expect(signupSchema.safeParse({ ...valid, password: "x".repeat(128) }).success).toBe(true);
  });

  it("limits the workspace name to 100 characters", () => {
    expect(signupSchema.safeParse({ ...valid, tenant_name: "x".repeat(101) }).success).toBe(false);
    expect(signupSchema.safeParse({ ...valid, tenant_name: "   " }).success).toBe(false);
  });
});
