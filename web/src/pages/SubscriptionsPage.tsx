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
      setActionError(err instanceof ApiError ? err.message : "操作失败，请重试");
    }
  };

  if (!subs) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? "暂时无法获取订阅数据，请稍后刷新重试" : "数据加载中…"}
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
            <p className="text-xs text-muted-foreground">订阅总数</p>
            <p className="text-base font-semibold">{subs?.length ?? "—"}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">启用中</p>
            <p className="text-base font-semibold">
              {subs ? subs.filter((v) => v.enabled).length : "—"}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">已停用</p>
            <p className="text-base font-semibold">
              {subs ? subs.filter((v) => !v.enabled).length : "—"}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">在线 IP 总数</p>
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
                ["list", "列表"],
                ["card", "卡片"],
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
            新建订阅
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
          onCopyLink={(v) => void copyText(subLink(v), "订阅链接已复制")}
          onQr={(v) => setQrSub(v)}
          onEdit={(v) => setSubDialog(v)}
          onResetLink={(v) => setPendingResetLink(v)}
          onResetAll={(v) => setPendingResetAll(v)}
          onToggleEnabled={(v) =>
            void runAction(
              v.id,
              () => api.subscriptions.setEnabled(v.id, !v.enabled),
              v.enabled ? "订阅已停用" : "订阅已启用",
            )
          }
          onDelete={(v) => setPendingDeleteSub(v)}
        />
      ) : (
        <SubscriptionCards
          subs={subs}
          templates={templates}
          onlineCounts={onlineCounts}
          onCopyLink={(v) => void copyText(subLink(v), "订阅链接已复制")}
          onQr={(v) => setQrSub(v)}
          onEdit={(v) => setSubDialog(v)}
          onResetLink={(v) => setPendingResetLink(v)}
          onResetAll={(v) => setPendingResetAll(v)}
          onToggleEnabled={(v) =>
            void runAction(
              v.id,
              () => api.subscriptions.setEnabled(v.id, !v.enabled),
              v.enabled ? "订阅已停用" : "订阅已启用",
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
        title="删除订阅"
        description={
          pendingDeleteSub
            ? `确定要删除订阅「${pendingDeleteSub.name}」吗？删除后该订阅的链接将不可用。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={() => {
          if (!pendingDeleteSub) return;
          const v = pendingDeleteSub;
          setPendingDeleteSub(null);
          void runAction(v.id, () => api.subscriptions.remove(v.id), "订阅已删除");
        }}
      />
      <ConfirmDialog
        open={pendingResetLink !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetLink(null);
        }}
        title="更换订阅链接"
        description={
          pendingResetLink
            ? `确定要更换订阅「${pendingResetLink.name}」的链接吗？旧链接将立即失效；已导入的客户端可继续连接，但更新订阅需换用新链接。`
            : ""
        }
        confirmLabel="更换"
        destructive
        onConfirm={() => {
          if (!pendingResetLink) return;
          const v = pendingResetLink;
          setPendingResetLink(null);
          void runAction(
            v.id,
            () => api.subscriptions.resetToken(v.id),
            "链接已更换，旧链接已失效",
          );
        }}
      />
      <ConfirmDialog
        open={pendingResetAll !== null}
        onOpenChange={(open) => {
          if (!open) setPendingResetAll(null);
        }}
        title="重置订阅链接"
        description={
          pendingResetAll
            ? `确定要重置订阅「${pendingResetAll.name}」吗？将同时更换订阅链接和 UUID，旧链接与旧配置立即失效，所有用户需重新订阅。`
            : undefined
        }
        confirmLabel="重置"
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
                err instanceof ApiError ? err.message : "操作失败，请重试",
              );
              return;
            }
            try {
              await api.subscriptions.resetToken(v.id);
            } catch {
              await refresh();
              setActionError("UUID 已更换，链接更换失败，请重试");
              return;
            }
            await refresh();
            toast.success("链接与 UUID 已全部重置，旧配置已失效");
          })();
        }}
      />
    </div>
  );
}
