import type { SubscriptionView } from "@/lib/api";
import { cn } from "@/lib/utils";
import i18n from "@/lib/i18n";

export function StatusPills({ v }: { v: SubscriptionView }) {
  const life = {
    active: [i18n.t('subscriptions.k035'), "text-emerald-600 dark:text-emerald-400", "bg-emerald-500"],
    disabled: [i18n.t('subscriptions.k036'), "text-muted-foreground", "bg-muted-foreground"],
    expired: [i18n.t('subscriptions.k037'), "text-destructive", "bg-destructive"],
    over_quota: [i18n.t('subscriptions.k038'), "text-destructive", "bg-destructive"],
  }[v.status] as [string, string, string];
  return (
    <div className="flex items-center gap-1.5">
      <span
        className={cn(
          "inline-flex items-center gap-1.5 rounded-full bg-muted px-2 py-0.5 text-xs font-medium",
          life[1],
        )}
      >
        <span className={cn("h-1.5 w-1.5 rounded-full", life[2])} />
        {life[0]}
      </span>
    </div>
  );
}

// Copy + QR + dropdown, shared by the table row and the card: one
// source so the two layouts can never drift apart in behaviour.
