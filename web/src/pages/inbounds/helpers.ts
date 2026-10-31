import type { LocalMachine } from "@/lib/api";

export function randomHex(bytes: number): string {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
}

// Row action trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background. Disabled triggers add
// opacity-50 for affordance.

export function formatLocalLabel(lm: LocalMachine): string {
  if (!lm.hostname) return "本机";
  if (lm.ip && lm.port > 0) return `${lm.hostname}（${lm.ip}:${lm.port}）`;
  if (lm.ip) return `${lm.hostname}（${lm.ip}）`;
  return lm.hostname;
}
