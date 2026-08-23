/** The problem with a whole attempt, above the fields. role="alert" has screen readers announce it. */
export function FormError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-sm border border-bill bg-bill-tint px-3.5 py-3 text-sm font-medium text-bill">
      {message}
    </p>
  );
}
