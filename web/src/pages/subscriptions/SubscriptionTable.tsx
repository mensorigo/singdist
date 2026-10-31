import { cn } from "@/lib/utils";
import { RowActions } from "./RowActions";
import { StatusPills } from "./StatusPills";
import { TrafficMeter } from "./TrafficMeter";
import { UuidCopy } from "./UuidCopy";
import { expiryCountdown, fmtDate, templateOf } from "./format";
import type { SubscriptionViewsProps } from "./views";

export function SubscriptionTable({
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
        <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
          <div className="overflow-x-auto">
          <table className="w-full min-w-[980px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">订阅</th>
                <th className="py-2 pr-3 font-medium">模板</th>
                <th className="py-2 pr-3 font-medium">状态</th>
                <th className="py-2 pr-3 font-medium">在线 IP</th>
                <th className="py-2 pr-3 font-medium">流量</th>
                <th className="py-2 pr-3 font-medium">到期</th>
                <th className="py-2 pr-3 font-medium">UUID</th>
                <th className="py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {subs.map((v) => {
                const t = templateOf(v, templates);
                const cd = expiryCountdown(v.expireAt);
                return (
                  <tr key={v.id} className="border-b last:border-0">
                    <td
                      className={cn(
                        "py-2.5 pr-3 font-medium",
                        !v.enabled && "text-muted-foreground",
                      )}
                    >
                      {v.name}
                    </td>
                    <td className="py-2.5 pr-3">
                      <p
                        className={cn(
                          !v.enabled && "text-muted-foreground",
                        )}
                      >
                        {t.name}
                      </p>
                    </td>
                    <td className="py-2.5 pr-3">
                      <StatusPills v={v} />
                    </td>
                    <td className="py-2.5 pr-3 tabular-nums text-muted-foreground">
                      {onlineCounts?.[v.id] ?? "—"}
                    </td>
                    <td className="py-2.5 pr-3">
                      <TrafficMeter v={v} />
                    </td>
                    <td className="py-2.5 pr-3 tabular-nums">
                      <p
                        className={cn(
                          v.expireAt > 0
                            ? "font-medium"
                            : "text-muted-foreground",
                        )}
                      >
                        {fmtDate(v.expireAt)}
                      </p>
                      {cd && (
                        <p className="text-xs text-muted-foreground">{cd}</p>
                      )}
                    </td>
                    <td className="py-2.5 pr-3">
                      <UuidCopy v={v} />
                    </td>
                    <td className="py-2.5">
                      <div className="flex items-center justify-end">
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
                    </td>
                  </tr>
                );
              })}
              {subs.length === 0 && (
                <tr>
                  <td
                    colSpan={8}
                    className="py-6 text-center text-sm text-muted-foreground"
                  >
                    暂无订阅，点右上角「新建订阅」创建
                    {templates.length === 0 && "（需要先在「入站 → 模板」中新建模板）"}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        </section>
  );
}
