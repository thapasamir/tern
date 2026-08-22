import type { components } from "./schema";

type ErrorBody = components["schemas"]["Error"]["error"];

/** The machine-readable reasons the API gives, straight from the OpenAPI document. */
export type ApiErrorCode = ErrorBody["code"];

/**
 * A failed call to the Tern API, in the shape the API reports it. Pages let it bubble to an error
 * boundary; forms read `code` and `message` to show something specific.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode | "unknown";
  /** Quote this when asking for help: it finds the request in the server's logs. */
  readonly requestId?: string;

  constructor(init: { status: number; code: ApiErrorCode | "unknown"; message: string; requestId?: string }) {
    super(init.message);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.requestId = init.requestId;
  }

  /** True for the API's "no such thing" answer, which a page shows as 404 rather than an error. */
  get isNotFound() {
    return this.code === "not_found";
  }
}

/**
 * Builds an ApiError from a response body. A body that is not the API's error shape (a gateway's
 * HTML page, say) still yields a usable error, so callers never have to handle "no error".
 */
export function toApiError(status: number, body: unknown): ApiError {
  const parsed = readErrorBody(body);
  if (parsed) {
    return new ApiError({ status, code: parsed.code, message: parsed.message, requestId: parsed.request_id });
  }
  return new ApiError({ status, code: "unknown", message: `The server answered ${status} unexpectedly.` });
}

function readErrorBody(body: unknown): ErrorBody | undefined {
  if (typeof body !== "object" || body === null || !("error" in body)) return undefined;
  const inner = (body as { error: unknown }).error;
  if (typeof inner !== "object" || inner === null) return undefined;
  const { code, message } = inner as Record<string, unknown>;
  if (typeof code !== "string" || typeof message !== "string") return undefined;
  const requestId = (inner as Record<string, unknown>).request_id;
  return {
    code: code as ApiErrorCode,
    message,
    request_id: typeof requestId === "string" ? requestId : undefined,
  };
}
