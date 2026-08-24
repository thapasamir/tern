"use server";

import { revalidatePath } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError } from "@/shared/api/errors";
import type { FormState } from "@/shared/forms/form-state";
import { parseForm } from "@/shared/forms/parse-form";

import { createEndpoint, deleteEndpoint, replayFailed, rotateSecret, setEnabled, updateEndpoint } from "./api";
import { describeEndpointError } from "./messages";
import { endpointSchema, replaySchema } from "./schemas";

/*
 * Server actions behind the endpoint screens. A problem the visitor can fix comes back as state for
 * the form to show; anything else is thrown to the error boundary. Actions that change what a page
 * shows revalidate it, so the page is fresh when the action finishes.
 */

const FIELDS = ["url", "description", "event_types"];

/** The state a form receives after a failed call: the problem, and what was typed. */
function failed(error: unknown, formData?: FormData): FormState {
  if (!(error instanceof ApiError)) throw error;
  const values = formData && Object.fromEntries(FIELDS.map((name) => [name, formData.get(name)?.toString() ?? ""]));
  return { error: describeEndpointError(error), values };
}

export type CreatedState = FormState<{ created?: { id: string; secret: string } }>;

export async function createEndpointAction(_previous: CreatedState, formData: FormData): Promise<CreatedState> {
  const form = parseForm(endpointSchema, formData, FIELDS);
  if (!form.ok) return form.state;
  try {
    const { id, secret } = await createEndpoint(form.data);
    revalidatePath("/endpoints");
    return { created: { id, secret } };
  } catch (error) {
    return failed(error, formData);
  }
}

export type SavedState = FormState<{ saved?: boolean }>;

export async function updateEndpointAction(id: string, _previous: SavedState, formData: FormData): Promise<SavedState> {
  const form = parseForm(endpointSchema, formData, FIELDS);
  if (!form.ok) return form.state;
  try {
    await updateEndpoint(id, form.data);
  } catch (error) {
    return failed(error, formData);
  }
  revalidatePath(`/endpoints/${id}`);
  revalidatePath("/endpoints");
  return { saved: true };
}

export async function setEnabledAction(id: string, enabled: boolean): Promise<void> {
  await setEnabled(id, enabled);
  revalidatePath(`/endpoints/${id}`);
  revalidatePath("/endpoints");
}

export async function deleteEndpointAction(id: string): Promise<void> {
  await deleteEndpoint(id);
  revalidatePath("/endpoints");
  redirect("/endpoints");
}

export type RotatedState = FormState<{ secret?: string }>;

export async function rotateSecretAction(id: string): Promise<RotatedState> {
  try {
    const { secret } = await rotateSecret(id);
    return { secret };
  } catch (error) {
    return failed(error);
  }
}

export type ReplayState = FormState<{ replayed?: number; hasMore?: boolean }>;

export async function replayFailedAction(id: string, _previous: ReplayState, formData: FormData): Promise<ReplayState> {
  const form = parseForm(replaySchema, formData, ["window"]);
  if (!form.ok) return form.state;
  try {
    const { replayed, has_more } = await replayFailed(id, new Date(Date.now() - Number(form.data.window) * 1000));
    revalidatePath(`/endpoints/${id}`);
    return { replayed, hasMore: has_more };
  } catch (error) {
    return failed(error);
  }
}
