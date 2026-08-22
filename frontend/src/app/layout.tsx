import type { Metadata } from "next";
import { Familjen_Grotesk, Spline_Sans_Mono } from "next/font/google";

import "./globals.css";

// The interface face, and the face for everything a machine produced (ids, times, codes).
const familjen = Familjen_Grotesk({ variable: "--font-familjen", subsets: ["latin"] });
const splineMono = Spline_Sans_Mono({ variable: "--font-spline-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: { default: "Tern", template: "%s · Tern" },
  description: "Reliable webhook delivery: signed, retried, logged.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${familjen.variable} ${splineMono.variable}`}>
      <body className="min-h-dvh">{children}</body>
    </html>
  );
}
