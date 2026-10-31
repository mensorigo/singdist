import type { ReactNode } from "react";

export function SettingCard({
  icon,
  title,
  desc,
  children,
}: {
  icon: ReactNode;
  title: string;
  desc?: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex items-center gap-2.5">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          {icon}
        </span>
        <p className="text-base font-semibold">{title}</p>
      </div>
      {desc && (
        <p className="mt-2 text-[13px] text-muted-foreground">{desc}</p>
      )}
      <div className="mt-5">{children}</div>
    </section>
  );
}

