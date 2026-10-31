import type { InboundStatus } from "@/lib/api";
import { cn } from "@/lib/utils";

export function StatusBadge({
  status,
  showWarnings,
}: {
  status: InboundStatus;
  showWarnings: boolean;
}) {
  const map = {
    synced: ["已同步", "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"],
    // Warnings off: the drifted badge keeps its label but drops the
    // yellow, so no amber remains anywhere on the page.
    drifted: [
      "有差异",
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    pending: ["未同步", "bg-muted text-muted-foreground"],
    unknown: ["无法对照", "bg-muted text-muted-foreground"],
    disabled: ["已停用", "bg-muted text-muted-foreground"],
    not_listening: [
      "未监听",
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    paused: ["已暂停", "bg-amber-500/10 text-amber-600 dark:text-amber-400"],
    offline: ["离线", "bg-muted text-muted-foreground"],
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
