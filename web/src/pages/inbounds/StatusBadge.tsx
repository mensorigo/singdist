import type { InboundStatus } from "@/lib/api";
import { cn } from "@/lib/utils";
import i18n from "@/lib/i18n";

export function StatusBadge({
  status,
  showWarnings,
}: {
  status: InboundStatus;
  showWarnings: boolean;
}) {
  const map = {
    synced: [i18n.t('inbounds.k051'), "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"],
    // Warnings off: the drifted badge keeps its label but drops the
    // yellow, so no amber remains anywhere on the page.
    drifted: [
      i18n.t('inbounds.k052'),
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    pending: [i18n.t('inbounds.k053'), "bg-muted text-muted-foreground"],
    unknown: [i18n.t('inbounds.k054'), "bg-muted text-muted-foreground"],
    disabled: [i18n.t('inbounds.k055'), "bg-muted text-muted-foreground"],
    not_listening: [
      i18n.t('inbounds.k056'),
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    paused: [i18n.t('inbounds.k057'), "bg-amber-500/10 text-amber-600 dark:text-amber-400"],
    offline: [i18n.t('inbounds.k058'), "bg-muted text-muted-foreground"],
  } as const;
  const [label, cls] = map[status];
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-0.5 text-xs font-medium",
        cls,
      )}
    >
      {label}
    </span>
  );
}
