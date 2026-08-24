import type { ApiError } from "@/shared/api/errors";

/**
 * What to tell a visitor when the API refused an endpoint change. A rejected URL comes with the
 * API's own reason (for instance that it points at a private address), which is worth showing as
 * it is; anything unexpected gets a plain sentence.
 */
export function describeEndpointError(error: ApiError): string {
  switch (error.code) {
    case "invalid_request":
      return error.message;
    case "not_found":
      return "That endpoint no longer exists.";
    case "conflict":
      return error.message;
    case "rate_limited":
      return "Too many requests. Wait a moment and try again.";
    default:
      return "Something went wrong on our side. Try again.";
  }
}
