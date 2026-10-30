import { Toaster as Sonner } from "sonner";

import type { Theme } from "@/lib/theme";

// Instant notifications, top-right of the page. Purely informational:
// they confirm that something succeeded (or failed) and disappear on
// their own — no click or acknowledgement needed. Fire them from
// anywhere with `import { toast } from "sonner"`.
//
// Placement contract: the gap to the top and right edges is fixed
// (24px); the width hugs the content up to a cap, and each toast has a
// comfortable minimum height so short messages still read as a solid
// card rather than a sliver.
export function Toaster({ theme }: { theme: Theme }) {
  return (
    <Sonner
      theme={theme}
      position="top-right"
      offset={24}
      mobileOffset={16}
      duration={2600}
      gap={12}
      visibleToasts={3}
      // Sonner's own CSS sets the toast corner radius from its
      // --border-radius variable (default 8px) with a selector
      // stronger than a Tailwind class, so the rounded-[18px]
      // class alone was ignored. Overriding the variable here
      // is what actually moves it to 18px.
      style={
        {
          "--width": "fit-content",
          "--border-radius": "18px",
        } as React.CSSProperties
      }
      toastOptions={{
        classNames: {
          toast:
            "min-h-[68px] min-w-[min(300px,calc(100vw-32px))] max-w-[min(420px,calc(100vw-32px))] rounded-[18px] border bg-background px-4 py-4 text-foreground shadow-lg [&_[data-content]]:ml-1.5 [&_[data-icon]]:shrink-0",
          title: "text-sm font-medium leading-snug",
          description: "text-[13px] leading-snug text-muted-foreground",
          success:
            "border-l-4 border-l-emerald-500 [&_[data-icon]]:text-emerald-500",
          error:
            "border-l-4 border-l-destructive [&_[data-icon]]:text-destructive",
          info: "border-l-4 border-l-sky-500 [&_[data-icon]]:text-sky-500",
          actionButton: "bg-primary text-primary-foreground",
          cancelButton: "bg-muted text-muted-foreground",
        },
      }}
    />
  );
}
