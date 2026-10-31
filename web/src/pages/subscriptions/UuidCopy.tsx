import type { SubscriptionView } from "@/lib/api";
import { copyText } from "./format";
import i18n from "@/lib/i18n";

export function UuidCopy({ v }: { v: SubscriptionView }) {
  return (
    <button
      type="button"
      title={i18n.t('subscriptions.k073', { value: v.uuid })}
      onClick={() => void copyText(v.uuid, i18n.t('subscriptions.k074'))}
      className="min-w-0 truncate font-mono text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      {v.uuid.slice(0, 19)}…
    </button>
  );
}

// Days left until expiry, by calendar date (expireAt is end-of-day):
// "" = never expires; "今天到期" / "剩 X 天" / "已过期 X 天".
