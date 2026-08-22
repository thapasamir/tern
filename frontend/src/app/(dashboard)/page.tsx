import { PageHeader } from "@/shared/ui/page-header";

export default function OverviewPage() {
  return (
    <>
      <PageHeader eyebrow="Overview" title="The last 24 hours" />
      <div className="flex max-w-xl flex-col items-start gap-2 rounded-sm border border-dashed border-wing px-6 py-7">
        <h2 className="text-[22px] font-semibold tracking-tight">Figures arrive in a later step</h2>
        <p className="text-[15px] leading-relaxed text-graphite">
          The shell, the design tokens and the API client are in place. The overview is built once sign-in and the
          delivery log exist.
        </p>
      </div>
    </>
  );
}
