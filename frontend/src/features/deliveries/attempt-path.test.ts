import { describe, expect, it } from "vitest";

import { attemptSucceeded, pathPositions } from "./attempt-path";

describe("pathPositions", () => {
  it("is empty without attempts", () => {
    expect(pathPositions([])).toEqual([]);
  });

  it("puts a single attempt at the start", () => {
    expect(pathPositions(["2026-08-13T09:41:07Z"])).toEqual([{ elapsedSeconds: 0, position: 0 }]);
  });

  it("runs from 0 to 100 and keeps the real elapsed time", () => {
    const points = pathPositions(["2026-08-13T09:41:07Z", "2026-08-13T09:41:12Z", "2026-08-14T13:16:26Z"]);

    expect(points.map((p) => p.elapsedSeconds)).toEqual([0, 5, 99319]);
    expect(points[0].position).toBe(0);
    expect(points[2].position).toBe(100);
  });

  it("keeps a 5 second wait visible next to a 27 hour one", () => {
    const [, second] = pathPositions(["2026-08-13T09:41:07Z", "2026-08-13T09:41:12Z", "2026-08-14T13:16:26Z"]);

    expect(second.position).toBeGreaterThan(5); // a linear scale would give 0.005
  });

  it("never goes backwards when times are equal", () => {
    expect(pathPositions(["2026-08-13T09:41:07Z", "2026-08-13T09:41:07Z"]).map((p) => p.position)).toEqual([0, 0]);
  });
});

describe("attemptSucceeded", () => {
  it.each([
    [200, true],
    [204, true],
    [299, true],
    [301, false],
    [410, false],
    [500, false],
    [null, false],
  ])("status %s -> %s", (code, expected) => {
    expect(attemptSucceeded(code)).toBe(expected);
  });
});
