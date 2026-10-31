import { dateLocale } from "@/lib/i18n";
export function fmtTime(ts: number): string {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString(dateLocale(), { hour12: false });
}

export function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

export function fmtMem(n: number): string {
  return fmtBytes(n);
}

// Card/menu trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background. Disabled triggers add
// opacity-50 for affordance; the baseline triggers are never disabled
// so they do not need it.

export const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// A bare IP/host without a scheme or port gets the default agent port
// before sending, so the backend always sees host:port. Inputs that
// already carry a scheme or a port (and bare IPv6) pass through
// untouched; the backend rejects those it cannot parse, as today.

export function normalizeServerIP(raw: string): string {
  const v = raw.trim();
  if (v === "") return v;
  if (/^https?:\/\//i.test(v)) return v;
  if (v.startsWith("[")) {
    return /\]:\d+$/.test(v) ? v : `${v}:32435`;
  }
  const colons = (v.match(/:/g) ?? []).length;
  if (colons === 0) return `${v}:32435`;
  if (colons === 1) {
    const after = v.split(":").pop() ?? "";
    if (/^\d+$/.test(after)) return v;
    if (after === "") return `${v}32435`;
    return v;
  }
  return v;
}
