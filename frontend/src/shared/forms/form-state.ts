/** What a form's server action hands back to the form: what went wrong, and what to keep on screen. */
export type FormState<Extra = object> = Extra & {
  /** A problem with the attempt as a whole ("wrong password"). */
  error?: string;
  /** Problems with single fields, keyed by field name. */
  fieldErrors?: Partial<Record<string, string>>;
  /** Echoed back so a failed attempt does not clear what was typed. Never a password. */
  values?: Partial<Record<string, string>>;
};
