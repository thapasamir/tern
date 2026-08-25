import { formatElapsed } from "@/shared/lib/format";

import { attemptSucceeded, pathPositions } from "../attempt-path";
import type { Attempt } from "../api";

const WIDTH = 1000;
const HEIGHT = 150;
const MARGIN = 56;
const LINE_Y = 75;

type Props = {
  attempts: Attempt[];
  /** When the next attempt is due, for a delivery still being retried: drawn as a ring at the end. */
  nextAttemptAt?: string | null;
};

/**
 * The signature drawing of Tern: a delivery's attempts along a time path. A cross is an attempt
 * that failed, a dot one that got through, a ring the attempt still to come. Distances are squeezed
 * (see pathPositions) so a 5 second wait and a 10 hour wait both stay visible; the labels give the
 * real elapsed time, and the table below the drawing has every number in text.
 */
export function AttemptPath({ attempts, nextAttemptAt }: Props) {
  if (attempts.length === 0) return null;

  const times = [...attempts.map((a) => a.started_at), ...(nextAttemptAt ? [nextAttemptAt] : [])];
  const points = pathPositions(times);
  const x = (position: number) => MARGIN + (position / 100) * (WIDTH - 2 * MARGIN);

  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      role="img"
      aria-label={`${attempts.length} ${attempts.length === 1 ? "attempt" : "attempts"} along a time path`}
      className="h-auto w-full"
    >
      <line x1={x(0)} x2={x(100)} y1={LINE_Y} y2={LINE_Y} className="stroke-ink" strokeWidth="1.5" />
      {points.map((point, i) => {
        const attempt = attempts[i] as Attempt | undefined;
        const cx = x(point.position);
        const above = i % 2 === 0;
        const labelY = above ? 28 : 128;
        const stemEnd = above ? labelY + 8 : labelY - 14;
        return (
          <g key={i}>
            <line x1={cx} x2={cx} y1={LINE_Y} y2={stemEnd} className="stroke-rule" strokeWidth="1" />
            {attempt ? (
              <Mark succeeded={attemptSucceeded(attempt.status_code)} cx={cx} cy={LINE_Y} />
            ) : (
              <circle cx={cx} cy={LINE_Y} r="7" className="fill-fog stroke-ochre" strokeWidth="2.5" />
            )}
            <text x={cx} y={labelY} textAnchor="middle" className="fill-ink font-mono text-[15px]">
              {attempt ? `#${i + 1} · ${attempt.status_code ?? "no reply"}` : "next"}
            </text>
            <text x={cx} y={labelY + 18} textAnchor="middle" className="fill-wing font-mono text-[13px]">
              {formatElapsed(point.elapsedSeconds)}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

function Mark({ succeeded, cx, cy }: { succeeded: boolean; cx: number; cy: number }) {
  if (succeeded) return <circle cx={cx} cy={cy} r="8" className="fill-sea" />;
  const r = 7;
  return (
    <path
      d={`M${cx - r} ${cy - r} L${cx + r} ${cy + r} M${cx + r} ${cy - r} L${cx - r} ${cy + r}`}
      className="stroke-bill"
      strokeWidth="3"
      fill="none"
    />
  );
}
