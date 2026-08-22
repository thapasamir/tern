import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { StatusBadge, type Status } from "@/shared/ui/status-badge";
import { TernMark } from "@/shared/ui/tern-mark";

export const metadata: Metadata = { title: "Styleguide" };

const palette = [
  ["fog", "#EDEFEA", "bg-fog"],
  ["paper", "#F8F9F6", "bg-paper"],
  ["ink", "#14181B", "bg-ink"],
  ["graphite", "#4A5358", "bg-graphite"],
  ["wing", "#5C666B", "bg-wing"],
  ["rule", "#C9CFCB", "bg-rule"],
  ["bill", "#B52A10", "bg-bill"],
  ["sea", "#17695C", "bg-sea"],
  ["ochre", "#7A4E00", "bg-ochre"],
] as const;

const statuses: Status[] = ["succeeded", "failed", "pending", "active", "disabled"];

/** Every shared component in one place, for checking them in a browser. Development only. */
export default function StyleguidePage() {
  if (process.env.NODE_ENV === "production") notFound();

  return (
    <main className="mx-auto flex max-w-[1280px] flex-col gap-12 px-5 py-12 sm:px-10">
      <header className="flex items-center justify-between border-b border-ink pb-5">
        <div className="flex items-center gap-3">
          <TernMark className="size-10" />
          <h1 className="text-3xl font-semibold tracking-tight">Styleguide</h1>
        </div>
        <span className="eyebrow">Development only</span>
      </header>

      <section className="flex flex-col gap-4">
        <h2 className="eyebrow">Palette</h2>
        <ul className="grid grid-cols-3 gap-3 sm:grid-cols-5 lg:grid-cols-9">
          {palette.map(([name, hex, swatch]) => (
            <li key={name} className="flex flex-col gap-1.5">
              <div className={`h-20 border border-rule ${swatch}`} />
              <span className="text-sm font-semibold">{name}</span>
              <span className="data text-xs text-wing">{hex}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="eyebrow">Buttons</h2>
        <div className="flex flex-wrap items-center gap-3">
          <Button>Send test event</Button>
          <Button variant="outline">Replay</Button>
          <Button variant="quiet">Cancel</Button>
          <Button variant="destructive">Delete endpoint</Button>
          <Button disabled>Disabled</Button>
          <Button size="sm" variant="outline">
            Small
          </Button>
        </div>
      </section>

      <section className="flex flex-col gap-4">
        <h2 className="eyebrow">Status</h2>
        <div className="flex flex-wrap items-center gap-3">
          {statuses.map((status) => (
            <StatusBadge key={status} status={status} />
          ))}
        </div>
      </section>

      <section className="flex max-w-md flex-col gap-4">
        <h2 className="eyebrow">Fields</h2>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="url-ok">Endpoint URL</Label>
          <Input id="url-ok" className="data text-sm" defaultValue="https://api.acme.dev/hooks/billing" />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="url-bad">Endpoint URL</Label>
          <Input id="url-bad" className="data text-sm" defaultValue="http://192.168.1.4/hook" aria-invalid="true" />
          <p className="text-[13px] font-medium text-bill">That address is private. Use a public https:// URL.</p>
        </div>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="eyebrow">Type</h2>
        <p className="text-5xl font-semibold tracking-[-0.025em]">Delivered, retried, replayed.</p>
        <p className="data text-sm">01a21138-45fa-77c4-9afc-d148ebfbd276 · 2026-08-14 20:55:03 · POST 503 · 2,104 ms</p>
      </section>
    </main>
  );
}
