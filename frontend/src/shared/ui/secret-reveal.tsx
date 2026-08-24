"use client";

import { useState } from "react";

import { Button } from "./button";

/**
 * A secret the server will never show again (a signing secret, an API key). It sits in a loud box
 * with a copy button, and says so. Leaving the page loses it, which is the point.
 */
export function SecretReveal({ title, secret, hint }: { title: string; secret: string; hint: string }) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(secret);
      setCopied(true);
    } catch {
      // No clipboard permission: the secret is selectable, so it can still be copied by hand.
    }
  }

  return (
    <section aria-labelledby="secret-title" className="flex flex-col gap-3 rounded-sm border-2 border-ink bg-paper p-5">
      <h2 id="secret-title" className="text-[22px] font-semibold tracking-tight">
        {title}
      </h2>
      <p className="text-[15px] text-graphite">{hint}</p>
      <div className="flex flex-wrap items-center gap-3">
        <code className="data min-w-0 flex-1 rounded-sm border border-rule bg-fog px-3.5 py-3 text-sm break-all select-all">
          {secret}
        </code>
        <Button type="button" variant="outline" onClick={copy}>
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <span role="status" className="sr-only">
        {copied ? "Copied to the clipboard" : ""}
      </span>
    </section>
  );
}
