import { TernMark } from "@/shared/ui/tern-mark";

/** The frame around signing in and up: the mark, then a single narrow column. */
export default function AuthLayout({ children }: LayoutProps<"/">) {
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-[440px] flex-col justify-center gap-10 px-5 py-16">
      <div className="flex items-center gap-2.5">
        <TernMark />
        <span className="text-2xl font-semibold tracking-tight">tern</span>
      </div>
      {children}
    </main>
  );
}
