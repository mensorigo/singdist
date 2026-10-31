import { cn } from "@/lib/utils";
import i18n from "@/lib/i18n";

// EnabledSwitch is a small hand-rolled toggle (no extra dependency),
// shared by the list pages that have an enable/disable flag.
export function EnabledSwitch({
  enabled,
  disabled,
  onToggle,
}: {
  enabled: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={enabled}
      aria-label={enabled ? i18n.t('common.k004') : i18n.t('common.k005')}
      title={enabled ? i18n.t('common.k006') : i18n.t('common.k007')}
      disabled={disabled}
      onClick={onToggle}
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full transition-colors duration-200 disabled:opacity-50",
        // Dark mode needs its own track colors: dark --primary is
        // near-white, so the old bg-primary on-state rendered white
        // track + white thumb (indistinguishable, glaring). Light
        // mode classes are untouched.
        enabled
          ? "bg-primary dark:bg-emerald-600"
          : "bg-muted-foreground/30 dark:bg-zinc-700",
      )}
    >
      <span
        className={cn(
          "absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform duration-200",
          enabled && "translate-x-4",
        )}
      />
    </button>
  );
}
