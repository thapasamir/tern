import type { Metadata } from "next";
import { Suspense } from "react";

import { redirectIfSignedIn } from "@/features/auth/account";
import { LoginForm } from "@/features/auth/components/login-form";
import { PageHeader } from "@/shared/ui/page-header";

export const metadata: Metadata = { title: "Sign in" };

export default function LoginPage({ searchParams }: PageProps<"/login">) {
  return (
    <>
      <PageHeader eyebrow="Welcome back" title="Sign in" />
      {/* Reads the cookie and the query string, so it streams in behind a boundary. */}
      <Suspense>
        <LoginView searchParams={searchParams} />
      </Suspense>
    </>
  );
}

async function LoginView({ searchParams }: { searchParams: PageProps<"/login">["searchParams"] }) {
  const { next } = await searchParams;
  const target = typeof next === "string" ? next : undefined;
  await redirectIfSignedIn(target);
  return <LoginForm next={target} />;
}
