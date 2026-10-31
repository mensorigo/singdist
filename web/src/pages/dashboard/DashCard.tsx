import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function DashCard({
  icon,
  label,
  value,
  sub,
  barPct,
  dot,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  sub?: string;
  barPct?: number;
  dot?: "on" | "off";
}) {
  return (
    <div className="flex h-full flex-col gap-0 rounded-[18px] border bg-card py-6 text-card-foreground shadow-sm">
      <div className="flex flex-col space-y-3 px-6">
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-foreground/70">{icon}</span>
          <span className="text-sm text-muted-foreground">{label}</span>
          {dot && (
            <span
              className={cn(
                "ml-auto inline-block h-3 w-3 shrink-0 rounded-full",
                dot === "on" ? "bg-emerald-500" : "bg-red-500",
              )}
            />
          )}
        </div>
        <div className="truncate text-2xl font-semibold leading-8 tracking-tight tabular-nums">
          {value}
        </div>
        {sub && (
          <p className="line-clamp-2 text-xs leading-4 text-muted-foreground">
            {sub}
          </p>
        )}
        {barPct !== undefined && (
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-secondary">
            <div
              className="h-full rounded-full bg-primary transition-all duration-500"
              style={{ width: `${Math.max(0, Math.min(100, barPct))}%` }}
            />
          </div>
        )}
      </div>
    </div>
  );
}

