import { cn } from "@/lib/utils";

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
      aria-label={enabled ? "停用" : "启用"}
      title={enabled ? "已启用（点击停用）" : "已停用（点击启用）"}
      disabled={disabled}
      onClick={onToggle}
      className={cn(
        "relative h-5 w-9 shrink-0 rounded-full transition-colors duration-200 disabled:opacity-50",
        enabled ? "bg-primary" : "bg-muted-foreground/30",
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
