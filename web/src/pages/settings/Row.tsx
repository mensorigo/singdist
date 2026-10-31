import type { ReactNode } from "react";

// Row: label on the left, control on the right, hairline between rows.
export function Row({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-2 py-3.5 first:pt-0 last:pb-0">
      <div className="w-40 shrink-0">
        <p className="text-sm">{label}</p>
        {hint && (
          <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-3">
        {children}
      </div>
    </div>
  );
}

