"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { replayDelivery } from "./api";

/** Replays a delivery and opens the new one, which is where its progress shows. */
export async function replayDeliveryAction(id: string): Promise<void> {
  const replay = await replayDelivery(id);
  revalidatePath("/deliveries");
  redirect(`/deliveries/${replay.id}`);
}
