// SyncControl lives in the top bar (right cluster) on every page: syncing
// applies the whole panel's desired state to the core (inbounds,
// subscriptions, templates, certificates), so it belongs to the panel,
// not to any single page. Compact by design: a status dot and a 同步
// button, nothing else. (A history-versions menu with rollback used to
// live here; it was removed 2026-10-24 — every sync archived a config
// copy, so a short auto-sync interval would pile up archive data for
// no real benefit.)
import { ArrowDownUp, LoaderCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { api, ApiError, onConfigChanged, type SyncStatus } from "@/lib/api";
import { cn } from "@/lib/utils";

export default function SyncControl({
  refreshSignal,
  onSynced,
}: {
  refreshSignal: number;
  onSynced: () => void;
}) {
  const [status, setStatus] = useState<SyncStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmSync, setConfirmSync] = useState(false);

  // Signature of the last seen status. Every successful load refreshes
  // this baseline; the poll compares against the pre-load value, so a
  // server-side change is caught even if it lands before the first poll.
  const lastSignature = useRef<string | null>(null);

  const load = useCallback(async () => {
    try {
      const s = await api.sync.status();
      setStatus(s);
      lastSignature.current = `${s.inSync}:${s.desiredHash}:${s.targetHash}`;
      return s;
    } catch {
      // The control stays quiet when the engine is unreachable; the
      // top bar must not break on an engine error.
      return null;
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load, refreshSignal]);

  // Live updates, no manual refresh needed: any successful config
  // mutation in this tab reloads the status immediately, and a 30s
  // poll covers everything else (auto-sync ticks, other tabs,
  // subscriptions expiring on their own). A poll that observes a
  // status change also bumps the global refresh so the open page
  // refetches its own banners and badges instead of going stale.
  useEffect(() => {
    const off = onConfigChanged(() => {
      void load();
    });
    const timer = setInterval(() => {
      void (async () => {
        const prev = lastSignature.current;
        const s = await load();
        if (!s) return;
        const sig = `${s.inSync}:${s.desiredHash}:${s.targetHash}`;
        if (prev !== null && prev !== sig) {
          onSynced();
        }
      })();
    }, 30_000);
    return () => {
      off();
      clearInterval(timer);
    };
  }, [load, onSynced]);

  const runSync = () => {
    setConfirmSync(false);
    setBusy(true);
    void (async () => {
      try {
        const row = await api.sync.run();
        if (row.result === "applied") {
          toast.success("已同步到核心");
        } else if (row.result === "no_change") {
          toast.success("配置无变更");
        } else {
          toast.error(row.detail || "同步失败");
        }
      } catch (err) {
        toast.error(err instanceof ApiError ? err.message : "同步失败，请重试");
      } finally {
        setBusy(false);
        await load();
        onSynced();
      }
    })();
  };

  return (
    <div className="flex h-10 shrink-0 items-center gap-1 rounded-full border bg-muted/40 py-1 pl-3 pr-1">
      <span
        title={
          !status
            ? "同步状态加载中"
            : status.inSync
              ? "核心配置已同步"
              : "有变更待同步"
        }
        className={cn(
          "h-2 w-2 shrink-0 rounded-full",
          !status
            ? "bg-muted-foreground/50"
            : status.inSync
              ? "bg-emerald-500"
              : "bg-amber-500",
        )}
      />
      <Button
        variant="ghost"
        size="icon"
        onClick={() => setConfirmSync(true)}
        disabled={busy || !status || status.inSync}
        className="h-8 w-8 shrink-0 rounded-full [&_svg]:size-5"
        title="同步到核心"
        aria-label="同步到核心"
      >
        {busy ? (
          <LoaderCircle strokeWidth={1.75} className="animate-spin" />
        ) : (
          <ArrowDownUp strokeWidth={1.75} />
        )}
      </Button>

      <ConfirmDialog
        open={confirmSync}
        onOpenChange={setConfirmSync}
        title="同步到核心"
        description={
          status?.restartEnabled
            ? "将按面板的入站与订阅重新生成核心配置并重启核心：校验不过会自动放弃，重启后不健康会自动回滚。继续吗？"
            : "将按面板的入站与订阅重新生成核心配置并写入目标文件（当前未配置核心重启，不会重启核心）。继续吗？"
        }
        confirmLabel="同步"
        busy={busy}
        onConfirm={runSync}
      />
    </div>
  );
}
