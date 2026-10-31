import { toast } from "sonner";
import type { SubscriptionView, TemplateView } from "@/lib/api";

export function expiryCountdown(expireAt: number): string {
  if (expireAt <= 0) return "";
  const day = 86400000;
  const today = new Date();
  today.setHours(0, 0, 0, 0);
  const exp = new Date(expireAt * 1000);
  exp.setHours(0, 0, 0, 0);
  const diff = Math.round((exp.getTime() - today.getTime()) / day);
  if (diff < 0) return `已过期 ${-diff} 天`;
  if (diff === 0) return "今天到期";
  return `剩 ${diff} 天`;
}

// Template name + inbound count for a subscription; falls back to the
// view's template name when the template row is missing.

export function templateOf(v: SubscriptionView, templates: TemplateView[]) {
  const t = templates.find((t) => t.id === v.templateId);
  if (t) return { name: t.name, inboundCount: t.inboundIds.length };
  return { name: v.templateName || "—", inboundCount: null as number | null };
}

export function fmtDate(unix: number): string {
  if (unix === 0) return "永不过期";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

// Old-panel byte style: always two decimals ("1.74 GB", "1.00 TB").

export function fmtBytes2(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  return `${(bytes / 1024 ** i).toFixed(2)} ${units[i]}`;
}

export function unixToDateInput(unix: number): string {
  if (unix === 0) return "";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function dateInputToUnix(v: string): number {
  if (!v) return 0;
  const t = new Date(`${v}T23:59:59`).getTime();
  return Number.isNaN(t) ? 0 : Math.floor(t / 1000);
}

export const UNITS = [
  { key: "MB", factor: 1024 ** 2 },
  { key: "GB", factor: 1024 ** 3 },
  { key: "TB", factor: 1024 ** 4 },
] as const;
export type UnitKey = (typeof UNITS)[number]["key"];

export function bytesToInput(bytes: number): { value: number; unit: UnitKey } {
  if (bytes === 0) return { value: 0, unit: "GB" };
  for (const u of [...UNITS].reverse()) {
    if (bytes % u.factor === 0) return { value: bytes / u.factor, unit: u.key };
  }
  const gb = UNITS[1];
  return { value: Math.round((bytes / gb.factor) * 100) / 100, unit: "GB" };
}

export async function copyText(text: string, okMsg: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(okMsg);
  } catch {
    toast.error("复制失败，请手动复制");
  }
}
