import { ApiError } from "@/shared/api/errors";

type Attempt = "login" | "signup";

/**
 * What to tell a visitor when the API refused a sign-in or sign-up. The API's own messages are
 * written for developers; these are for the person at the keyboard. A wrong password and an unknown
 * email share one message on purpose, as they do in the API.
 */
export function describeAuthError(error: ApiError, attempt: Attempt): string {
  switch (error.code) {
    case "unauthorized":
      return "That email and password don't match.";
    case "conflict":
      return "There is already an account with that email. Sign in instead.";
    case "rate_limited":
      return "Too many attempts from your network. Wait a minute and try again.";
    case "invalid_request":
      return error.message;
    default:
      return attempt === "login"
        ? "We couldn't sign you in. Try again."
        : "We couldn't create your account. Try again.";
  }
}
