import { cn } from "@/lib/utils";
import { RowActions } from "./RowActions";
import { StatusPills } from "./StatusPills";
import { TrafficMeter } from "./TrafficMeter";
import { UuidCopy } from "./UuidCopy";
import { expiryCountdown, fmtDate, templateOf } from "./format";
import type { SubscriptionViewsProps } from "./views";
import i18n from "@/lib/i18n";

export function SubscriptionCards({
  subs,
  templates,
  onlineCounts,
  onCopyLink,
  onQr,
  onEdit,
  onResetLink,
  onResetAll,
  onToggleEnabled,
  onDelete,
}: SubscriptionViewsProps) {
  return (
        // Card template: the same shared pieces, only arranged
        // differently — small rectangles in a grid (1 col on phones,
        // 2 on sm, 3 on xl), name + status on top, traffic bar full
        // width, expiry + UUID on a muted detail line.
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {subs.map((v) => {
            const t = templateOf(v, templates);
            const cd = expiryCountdown(v.expireAt);
            return (
            <article
              key={v.id}
              className={cn(
                "min-w-0 rounded-[18px] border bg-card px-4 py-4 shadow-sm",
                !v.enabled && "opacity-60",
              )}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate text-[16px] font-semibold">
                    {v.name}
                  </p>
                  <div className="mt-1.5">
                    <StatusPills v={v} />
                  </div>
                  <p className="mt-1.5 text-xs tabular-nums text-muted-foreground">
                    {i18n.t('subscriptions.k090', { value: onlineCounts?.[v.id] ?? "—" })}
                  </p>
                </div>
                <div className="shrink-0">
                  <RowActions
                    v={v}
                    onCopyLink={() =>
                      onCopyLink(v)
                    }
                    onQr={() => onQr(v)}
                    onEdit={() => onEdit(v)}
                    onResetLink={() => onResetLink(v)}
                    onResetAll={() => onResetAll(v)}
                    onToggleEnabled={() =>
                      onToggleEnabled(v)
                    }
                    onDelete={() => onDelete(v)}
                  />
                </div>
              </div>
              <div className="mt-3">
                <TrafficMeter v={v} barClassName="h-2 flex-1" />
              </div>
              <div className="mt-2 text-[13px] text-muted-foreground">
                {t.name}
                {t.inboundCount !== null && i18n.t('subscriptions.k039', { value: t.inboundCount })}
              </div>
              <div className="mt-1 flex flex-wrap items-center justify-between gap-3 text-[13px] text-muted-foreground">
                <span className="tabular-nums">
                  {v.expireAt > 0
                    ? i18n.t('subscriptions.k040', { value: fmtDate(v.expireAt), cd: cd })
                    : i18n.t('subscriptions.k041')}
                </span>
                <UuidCopy v={v} />
              </div>
            </article>
            );
          })}
          {subs.length === 0 && (
            <p className="col-span-full py-6 text-center text-sm text-muted-foreground">
              {i18n.t('subscriptions.k091')}
              {templates.length === 0 &&
                i18n.t('subscriptions.k042')}
            </p>
          )}
        </div>
  );
}
