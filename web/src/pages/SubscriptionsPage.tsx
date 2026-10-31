import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  api,
  ApiError,
  type SubscriptionView,
  type TemplateView,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import { QrDialog } from "./subscriptions/QrDialog";
import { SubscriptionFormDialog } from "./subscriptions/SubscriptionFormDialog";
import { SubscriptionTable } from "./subscriptions/SubscriptionTable";
import { SubscriptionCards } from "./subscriptions/SubscriptionCards";
import { copyText } from "./subscriptions/format";
import i18n from "@/lib/i18n";

export default function SubscriptionsPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [subs, setSubs] = useState<SubscriptionView[] | null>(null);
  const [templates, setTemplates] = useState<TemplateView[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  const [subDialog, setSubDialog] = useState<SubscriptionView | "new" | null>(
    null,
  );
  const [qrSub, setQrSub] = useState<SubscriptionView | null>(null);
  const [pendingDeleteSub, setPendingDeleteSub] =
    useState<SubscriptionView | null>(null);
  const [pendingResetAll, setPendingResetAll] =
    useState<SubscriptionView | null>(null);
  const [pendingResetLink, setPendingResetLink] =
    useState<SubscriptionView | null>(null);

  // List vs card layout, remembered per browser (same pattern as the
  // sidebar collapse). Default is the existing table: the card view is
  // opt-in, so nothing changes for anyone who never toggles.
  const [view, setView] = useState<"list" | "card">(() =>
    localStorage.getItem("singbox-ui-sub-view") === "card" ? "card" : "list",
  );
  const setViewPersist = (next: "list" | "card") => {
    setView(next);
    localStorage.setItem("singbox-ui-sub-view", next);
  };

  const refresh = useCallback(async () => {
    try {
      const [s, t] = await Promise.all([
        api.subscriptions.list(),
        api.templates.list(),
      ]);
      setSubs(s);
      setTemplates(t);
      setLoadError(false);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  // Online-IP counts on the dashboard interval timer (default 3s);
  // failures stay silent so a stats hiccup never blocks the list.
  const [onlineCounts, setOnlineCounts] = useState<Record<
    string,
    number
  > | null>(null);
  useEffect(() => {
    let cancelled = false;
    let timer: number | null = null;
    void (async () => {
      let intervalMs = 3000;
      try {
        const v = await api.settings.get();
        if (!cancelled) intervalMs = v.dashIntervalSecs * 1000;
      } catch {
        /* keep the default interval */
      }
      if (cancelled) return;
      const load = async () => {
        try {
          const r = await api.subscriptions.onlineIps();
          if (!cancelled) setOnlineCounts(r.counts);
        } catch {
          /* silent: stats only */
        }
      };
      void load();
      timer = window.setInterval(load, intervalMs);
    })();
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [refreshSignal]);

  const runAction = async (
    id: number,
    fn: () => Promise<unknown>,
    okMsg: string,
  ) => {
    void id;
    setActionError(null);
    try {
      await fn();
      await refresh();
      toast.success(okMsg);
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : i18n.t('subscriptions.k001'));
    }
  };

  if (!subs) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? i18n.t('subscriptions.k002') : i18n.t('subscriptions.k003')}
      </p>
    );
  }

  const subLink = (v: SubscriptionView) =>
    `${window.location.origin}/sub/${v.token}`;

  return (
    <div className="space-y-4">
      {/* Overview on the left, view switch + 新建订阅 on the
          right, both inside one card so the row lines up with
          the list cards below instead of floating bare. */}
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-[18px] border bg-card px-4 py-3 shadow-sm">
        <div className="flex flex-wrap items-center gap-6">
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('subscriptions.k004')}</p>
            <p className="text-base font-semibold">{subs?.length ?? "—"}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('subscriptions.k005')}</p>
            <p className="text-base font-semibold">
              {subs ? subs.filter((v) => v.enabled).length : "—"}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('subscriptions.k006')}</p>
            <p className="text-base font-semibold">
              {subs ? subs.filter((v) => !v.enabled).length : "—"}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('subscriptions.k007')}</p>
            <p className="text-base font-semibold">
              {onlineCounts
                ? Object.values(onlineCounts).reduce((s, c) => s + c, 0)
                : "—"}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {/* Same segmented control as the Inbounds page's 入站/模板
              switch: the view choice stays visible instead of hiding
              behind a single toggle icon. */}
          <div className="inline-flex rounded-xl bg-muted p-1">
            {(
              [
                ["list", i18n.t('subscriptions.k008')],
                ["card", i18n.t('subscriptions.k009')],
              ] as const
            ).map(([key, label]) => (
              <button
                key={key}
                type="button"
                onClick={() => setViewPersist(key)}
                className={cn(
                  "h-7 rounded-lg px-3.5 text-sm transition-colors",
                  view === key
                    ? "bg-background font-medium shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <Button
            onClick={() => setSubDialog("new")}
            className="h-8 rounded-xl px-3"
          >
            {i18n.t('subscriptions.k081')}
          </Button>
        </div>
      </div>
      {actionError && (
        <p className="text-[13px] text-destructive">{actionError}</p>
      )}
      {view === "list" ? (
        <SubscriptionTable
          subs={subs}
          templates={templates}
          onlineCounts={onlineCounts}
          onCopyLink={(v) => void copyText(subLink(v), i18n.t('subscriptions.k010'))}
          onQr={(v) => setQrSub(v)}
          onEdit={(v) => setSubDialog(v)}
          onResetLink={(v) => setPendingResetLink(v)}
          onResetAll={(v) => setPendingResetAll(v)}
          onToggleEnabled={(v) =>
            void runAction(
              v.id,
              () => api.subscriptions.setEnabled(v.id, !v.enabled),
              v.enabled ? i18n.t('subscriptions.k011') : i18n.t('subscriptions.k012'),
            )
          }
          onDelete={(v) => setPendingDeleteSub(v)}
        />
      ) : (
        <SubscriptionCards
          subs={subs}
          templates={templates}
          onlineCounts={onlineCounts}
          onCopyLink={(v) => void copyText(subLink(v), i18n.t('subscriptions.k010'))}
          onQr={(v) => setQrSub(v)}
          onEdit={(v) => setSubDialog(v)}
          onResetLink={(v) => setPendingResetLink(v)}
          onResetAll={(v) => setPendingResetAll(v)}
          onToggleEnabled={(v) =>
            void runAction(
              v.id,
              () => api.subscriptions.setEnabled(v.id, !v.enabled),
              v.enabled ? i18n.t('subscriptions.k011') : i18n.t('subscriptions.k012'),
            )
          }
          onDelete={(v) => setPendingDeleteSub(v)}
        />
      )}

      {subDialog !== null && (
        <SubscriptionFormDialog
          item={subDialog === "new" ? null : subDialog}
          templates={templates}
          onClose={() => setSubDialog(null)}
          onSaved={async () => {
            setSubDialog(null);
            await refresh();
          }}
        />
      )}
      {qrSub && <QrDialog sub={qrSub} onClose={() => setQrSub(null)} />}

      <ConfirmDialog
        open={pendingDeleteSub !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDeleteSub(null);
        }}
        title={i18n.t('subscriptions.k013')}
        description={
          pendingDeleteSub
            ? i18n.t('subscriptions.k014', { value: pendingDeleteSub.name })
            : undefined
        }
        confirmLabel={i18n.t('subscriptions.k015')}
        destructive
        onConfirm={() => {
          if (!pendingDeleteSub) return;
          const v = pendingDeleteSub;
          setPendingDeleteSub(null);
          void runAction(v.id, () => api.subscriptions.remove(v.id), i18n.t('subscriptions.k016'));
        }}
      />
      <ConfirmDialog
        open={pendingResetLink !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetLink(null);
        }}
        title={i18n.t('subscriptions.k017')}
        description={
          pendingResetLink
            ? i18n.t('subscriptions.k018', { value: pendingResetLink.name })
            : ""
        }
        confirmLabel={i18n.t('subscriptions.k019')}
        destructive
        onConfirm={() => {
          if (!pendingResetLink) return;
          const v = pendingResetLink;
          setPendingResetLink(null);
          void runAction(
            v.id,
            () => api.subscriptions.resetToken(v.id),
            i18n.t('subscriptions.k020'),
          );
        }}
      />
      <ConfirmDialog
        open={pendingResetAll !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetAll(null);
        }}
        title={i18n.t('subscriptions.k021')}
        description={
          pendingResetAll
            ? i18n.t('subscriptions.k022', { value: pendingResetAll.name })
            : undefined
        }
        confirmLabel={i18n.t('subscriptions.k023')}
        destructive
        onConfirm={() => {
          if (!pendingResetAll) return;
          const v = pendingResetAll;
          setPendingResetAll(null);
          // Sequential on purpose: credential first, then link. A plain
          // runAction would swallow which half succeeded, so handle the
          // partial failure explicitly: UUID done + link failed must say
          // so, because the subscription is then in a half-reset state.
          void (async () => {
            setActionError(null);
            try {
              await api.subscriptions.resetUuid(v.id);
            } catch (err) {
              setActionError(
                err instanceof ApiError ? err.message : i18n.t('subscriptions.k001'),
              );
              return;
            }
            try {
              await api.subscriptions.resetToken(v.id);
            } catch {
              await refresh();
              setActionError(i18n.t('subscriptions.k024'));
              return;
            }
            await refresh();
            toast.success(i18n.t('subscriptions.k025'));
          })();
        }}
      />
    </div>
  );
}
