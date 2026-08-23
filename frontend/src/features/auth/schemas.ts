import { z } from "zod";

/*
 * The same limits the API enforces (see openapi.yaml), checked first so a typo is answered at once
 * and with a message next to the field, instead of after a round trip.
 */

const email = z
  .string()
  .trim()
  .min(1, "Enter your email.")
  .max(254, "That email is too long.")
  .pipe(z.email("Enter a valid email address."));

export const loginSchema = z.object({
  email,
  password: z.string().min(1, "Enter your password."),
});

export const signupSchema = z.object({
  tenant_name: z.string().trim().min(1, "Name your workspace.").max(100, "Keep it under 100 characters."),
  email,
  password: z.string().min(8, "Use at least 8 characters.").max(128, "Use at most 128 characters."),
});

export type LoginInput = z.infer<typeof loginSchema>;
export type SignupInput = z.infer<typeof signupSchema>;
