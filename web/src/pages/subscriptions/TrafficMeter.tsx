import type { SubscriptionView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { fmtBytes2 } from "./format";
import i18n from "@/lib/i18n";

export function TrafficMeter({
  v,
  barClassName = "h-1.5 w-28",
}: {
  v: SubscriptionView;
  barClassName?: string;
}) {
  const pct =
    v.limitBytes > 0
      ? Math.min(100, (v.usedBytes / v.limitBytes) * 100)
      : 0;
  return (
    <div className="flex items-center gap-3">
      <div
        className={cn(
          "shrink-0 overflow-hidden rounded-full bg-muted",
          barClassName,
        )}
      >
        {v.limitBytes > 0 && pct > 0 && (
          <div
            className="h-full rounded-full bg-foreground/50"
            style={{ width: `${pct}%` }}
          />
        )}
      </div>
      <span className="whitespace-nowrap tabular-nums text-[13px] text-muted-foreground">
        {fmtBytes2(v.usedBytes)} /{" "}
        {v.limitBytes > 0
          ? `${fmtBytes2(v.limitBytes)} · ${pct.toFixed(1)}%`
          : i18n.t('subscriptions.k072')}
      </span>
    </div>
  );
}

// Truncated mono UUID, tap to copy — same in both layouts.
