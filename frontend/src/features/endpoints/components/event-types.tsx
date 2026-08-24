/** The event types an endpoint receives, as small mono tags; none means every event. */
export function EventTypes({ types }: { types: string[] }) {
  if (types.length === 0) return <span className="text-graphite">All events</span>;
  return (
    <ul className="flex flex-wrap gap-1.5">
      {types.map((type) => (
        <li key={type} className="data rounded-sm border border-rule bg-paper px-2 py-0.5 text-[13px]">
          {type}
        </li>
      ))}
    </ul>
  );
}
