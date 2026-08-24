const dateTime = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
  timeZone: "UTC",
});

/**
 * "25 Nov 2026, 07:47 UTC". Always UTC and always the same shape: the server renders these, so
 * there is no browser time zone to use, and log times from different people must be comparable.
 */
export function formatDateTime(iso: string): string {
  return `${dateTime.format(new Date(iso))} UTC`;
}
