import type { DashboardSummary } from "@/lib/api";
import { formatRate } from "@/lib/format";
import { TrafficChart } from "./TrafficChart";

export function RealtimeCard({ data }: { data: DashboardSummary }) {
  return (
    <div className="flex h-full flex-col rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="text-base font-semibold">实时网速</p>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-foreground" />
            上行 {formatRate(data.upRate)}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-muted-foreground" />
            下行 {formatRate(data.downRate)}
          </span>
        </div>
      </div>
      <div className="mt-3">
        <TrafficChart series={data.series} />
      </div>
    </div>
  );
}

/** Hand-rolled SVG line chart of the throughput series (no chart library). */
