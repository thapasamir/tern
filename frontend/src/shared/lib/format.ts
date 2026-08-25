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

const timestamp = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
  timeZone: "UTC",
});

/** "14 Aug, 14:02:11": a compact UTC time for log rows, where the year and the zone are in the header. */
export function formatTimestamp(iso: string): string {
  return timestamp.format(new Date(iso));
}

/** How long a request took: "118 ms" below a second, "2.1 s" above. */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`;
  const seconds = ms / 1000;
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} s`;
}

/** Time since a starting point, as "+5 s", "+35 min", "+2 h 35": the two largest units that matter. */
export function formatElapsed(seconds: number): string {
  if (seconds < 1) return "+0";
  if (seconds < 60) return `+${Math.round(seconds)} s`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `+${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `+${hours} h` : `+${hours} h ${rest}`;
}
