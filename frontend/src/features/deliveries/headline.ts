import type { DeliveryDetail } from "./api";

/** The one-line verdict on a delivery, for the top of its page. */
export function headline({ status, attempt_count: count }: Pick<DeliveryDetail, "status" | "attempt_count">): string {
  switch (status) {
    case "succeeded":
      return count <= 1 ? "Delivered on the first attempt" : `Delivered on attempt ${count}`;
    case "failed":
      return count === 1 ? "Failed on its only attempt" : `Failed after ${count} attempts`;
    case "pending":
      return count === 0
        ? "Waiting to be sent"
        : `Waiting to retry after ${count} failed ${count === 1 ? "attempt" : "attempts"}`;
  }
}
