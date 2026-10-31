import { useEffect, useState } from "react";
import { api, type TrendGranularity, type TrendPoint } from "@/lib/api";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";
import i18n, { dateLocale } from "@/lib/i18n";

const TREND_POLL_MS = 30000;

function granularities(): { key: TrendGranularity; label: string; range: string }[] {
  return [
  { key: "minute", label: i18n.t('dashboard.k022'), range: i18n.t('dashboard.k023') },
  { key: "hour", label: i18n.t('dashboard.k024'), range: i18n.t('dashboard.k025') },
  { key: "day", label: i18n.t('dashboard.k026'), range: i18n.t('dashboard.k027') },
];
}

export function TrendCard({
  refreshSignal,
  timezone,
}: {
  refreshSignal: number;
  timezone: string;
}) {
  const [granularity, setGranularity] = useState<TrendGranularity>("day");
  const [points, setPoints] = useState<TrendPoint[]>([]);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const t = await api.dashboardTrend(granularity);
        if (!cancelled) setPoints(t.points);
      } catch {
        /* keep showing the previous points */
      }
    }
    void load();
    const id = window.setInterval(load, TREND_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [granularity, refreshSignal]);

  const meta = granularities().find((g) => g.key === granularity)!;
  const max = Math.max(...points.map((p) => p.up + p.down), 1);

  // Trend labels render in the panel timezone, not the browser's, so
  // day boundaries match the backend buckets.
  const labelFor = (ts: number) => {
    const d = new Date(ts * 1000);
    if (granularity === "day") {
      return new Intl.DateTimeFormat(dateLocale(), {
        timeZone: timezone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(d);
    }
    return new Intl.DateTimeFormat(dateLocale(), {
      timeZone: timezone,
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(d);
  };

  return (
    <div className="flex h-full flex-col rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-base font-semibold">{i18n.t('dashboard.k028')}</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {meta.range} · {timezone}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-block h-2 w-2 rounded-full bg-foreground" />
            {i18n.t('dashboard.k033')}
          </span>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-block h-2 w-2 rounded-full bg-muted-foreground" />
            {i18n.t('dashboard.k034')}
          </span>
          <div className="flex rounded-lg bg-muted p-0.5">
            {granularities().map((g) => (
              <button
                key={g.key}
                onClick={() => setGranularity(g.key)}
                className={cn(
                  "h-6 rounded-md px-2.5 text-xs transition-colors",
                  granularity === g.key
                    ? "bg-background font-medium text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {g.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-3 flex h-[170px] flex-1 items-end gap-[3px] border-b">
        {points.map((p) => {
          const total = p.up + p.down;
          const totalPct = Math.max((total / max) * 100, total > 0 ? 1.5 : 0);
          const upShare = total > 0 ? (p.up / total) * 100 : 0;
          return (
            <div
              key={p.ts}
              className="flex h-full min-w-0 flex-1 flex-col items-center justify-end"
              title={i18n.t("dashboard.chartTitle", { time: labelFor(p.ts), up: formatBytes(p.up), down: formatBytes(p.down) })}
            >
              <div
                className="flex w-full max-w-6 flex-col justify-end overflow-hidden rounded-t-sm"
                style={{ height: `${totalPct}%` }}
              >
                <div
                  className="w-full shrink-0 bg-foreground"
                  style={{ height: `${upShare}%` }}
                />
                <div className="w-full flex-1 bg-muted-foreground/40" />
              </div>
            </div>
          );
        })}
        {points.length === 0 && (
          <div className="flex h-full w-full items-center justify-center text-[13px] font-medium text-muted-foreground">
            {i18n.t('dashboard.k035')}
          </div>
        )}
      </div>
      {points.length > 0 && (
        <div className="mt-2 flex justify-between text-xs text-muted-foreground">
          <span>{labelFor(points[0].ts)}</span>
          <span>{labelFor(points[points.length - 1].ts)}</span>
        </div>
      )}
    </div>
  );
}

