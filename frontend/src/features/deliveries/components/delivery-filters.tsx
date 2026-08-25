import Link from "next/link";

import { Button } from "@/shared/ui/button";
import { FormField } from "@/shared/ui/form-field";
import { Input } from "@/shared/ui/input";
import { Select } from "@/shared/ui/select";

import { type Filters, periods, statuses } from "../filters";

/**
 * The log's filters, as a plain GET form: submitting puts them in the address bar, so the view can
 * be shared and survives a refresh, and it works without any JavaScript.
 */
export function DeliveryFilters({
  filters,
  endpoints,
}: {
  filters: Filters;
  endpoints: { id: string; url: string }[];
}) {
  return (
    <form
      method="get"
      action="/deliveries"
      className="grid items-end gap-4 sm:grid-cols-2 lg:grid-cols-[2fr_1fr_1fr_2fr_auto]"
    >
      <FormField id="endpoint" label="Endpoint">
        <Select id="endpoint" name="endpoint" defaultValue={filters.endpoint ?? ""}>
          <option value="">All endpoints</option>
          {endpoints.map((e) => (
            <option key={e.id} value={e.id}>
              {e.url}
            </option>
          ))}
        </Select>
      </FormField>
      <FormField id="status" label="Status">
        <Select id="status" name="status" defaultValue={filters.status ?? ""}>
          <option value="">All</option>
          {statuses.map((s) => (
            <option key={s} value={s}>
              {s[0].toUpperCase() + s.slice(1)}
            </option>
          ))}
        </Select>
      </FormField>
      <FormField id="period" label="Period">
        <Select id="period" name="period" defaultValue={filters.period}>
          {periods.map((p) => (
            <option key={p.value} value={p.value}>
              {p.label}
            </option>
          ))}
        </Select>
      </FormField>
      <FormField id="event" label="Event id">
        <Input
          id="event"
          name="event"
          autoComplete="off"
          className="data"
          defaultValue={filters.event ?? ""}
          placeholder="01a2…"
        />
      </FormField>
      <div className="flex items-center gap-3">
        <Button type="submit">Apply</Button>
        <Link href="/deliveries" className="text-[15px] font-medium underline underline-offset-4">
          Clear
        </Link>
      </div>
    </form>
  );
}
