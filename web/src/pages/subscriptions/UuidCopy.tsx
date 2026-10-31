import type { SubscriptionView } from "@/lib/api";
import { copyText } from "./format";

export function UuidCopy({ v }: { v: SubscriptionView }) {
  return (
    <button
      type="button"
      title={`${v.uuid}（点击复制）`}
      onClick={() => void copyText(v.uuid, "UUID 已复制")}
      className="min-w-0 truncate font-mono text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      {v.uuid.slice(0, 19)}…
    </button>
  );
}

// Days left until expiry, by calendar date (expireAt is end-of-day):
// "" = never expires; "今天到期" / "剩 X 天" / "已过期 X 天".
