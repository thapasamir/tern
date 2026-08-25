/**
 * Where each attempt sits along the time path of a delivery, as a percentage from the first
 * attempt (0) to the last (100).
 *
 * The retry schedule runs from 5 seconds to 10 hours, so a linear scale would crush the early
 * attempts into one point. Raising elapsed time to a power below 1 squeezes the long waits and keeps
 * the short ones apart, and the labels say the real times.
 */
const SQUEEZE = 0.3;

export type PathPoint = { elapsedSeconds: number; position: number };

export function pathPositions(startTimes: string[]): PathPoint[] {
  if (startTimes.length === 0) return [];
  const first = new Date(startTimes[0]).getTime();
  const elapsed = startTimes.map((t) => Math.max(0, (new Date(t).getTime() - first) / 1000));
  const total = Math.pow(Math.max(...elapsed), SQUEEZE);
  return elapsed.map((seconds) => ({
    elapsedSeconds: seconds,
    position: total === 0 ? 0 : (100 * Math.pow(seconds, SQUEEZE)) / total,
  }));
}

/** An attempt succeeded when the endpoint answered with a 2xx. */
export function attemptSucceeded(statusCode: number | null): boolean {
  return statusCode !== null && statusCode >= 200 && statusCode < 300;
}
