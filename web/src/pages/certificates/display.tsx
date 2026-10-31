import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import i18n, { dateLocale } from "@/lib/i18n";

export function formatDateTime(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleString(dateLocale(), { hour12: false });
}

// Row action trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background. Disabled triggers add
// opacity-50 for affordance.
export const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

export function formatDate(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleDateString(dateLocale());
}

export function DaysBadge({
  daysLeft,
  expired,
  showWarnings,
}: {
  daysLeft: number;
  expired: boolean;
  showWarnings: boolean;
}) {
  if (expired) {
    return (
      <span className="inline-flex rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
        {i18n.t('certificates.k065')}
      </span>
    );
  }
  const cls =
    daysLeft < 15
      ? "bg-destructive/10 text-destructive"
      : daysLeft <= 60
        ? showWarnings
          ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
          : "bg-muted text-muted-foreground"
        : "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-0.5 text-xs font-medium tabular-nums",
        cls,
      )}
    >
      {i18n.t('certificates.k066', { daysLeft: daysLeft })}
    </span>
  );
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="mt-1 text-sm">{children}</div>
    </div>
  );
}

