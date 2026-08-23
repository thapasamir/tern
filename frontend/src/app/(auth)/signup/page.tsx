import type { Metadata } from "next";
import { Suspense } from "react";

import { redirectIfSignedIn } from "@/features/auth/account";
import { SignupForm } from "@/features/auth/components/signup-form";
import { PageHeader } from "@/shared/ui/page-header";

export const metadata: Metadata = { title: "Create an account" };

export default function SignupPage() {
  return (
    <>
      <PageHeader eyebrow="Get started" title="Create an account" />
      <Suspense>
        <SignupView />
      </Suspense>
    </>
  );
}

async function SignupView() {
  await redirectIfSignedIn();
  return <SignupForm />;
}
