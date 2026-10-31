import type { SubscriptionView, TemplateView } from "@/lib/api";

export interface SubscriptionViewsProps {
  subs: SubscriptionView[];
  templates: TemplateView[];
  onlineCounts: Record<string, number> | null;
  onCopyLink: (v: SubscriptionView) => void;
  onQr: (v: SubscriptionView) => void;
  onEdit: (v: SubscriptionView) => void;
  onResetLink: (v: SubscriptionView) => void;
  onResetAll: (v: SubscriptionView) => void;
  onToggleEnabled: (v: SubscriptionView) => void;
  onDelete: (v: SubscriptionView) => void;
}
