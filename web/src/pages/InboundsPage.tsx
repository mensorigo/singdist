import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  api,
  ApiError,
  type InboundList,
  type InboundView,
} from "@/lib/api";
import { cn } from "@/lib/utils";
import TemplatesPanel from "@/pages/TemplatesPanel";
import { InboundFormDialog } from "./inbounds/InboundFormDialog";
import { InboundListTable } from "./inbounds/InboundListTable";
import type { MachineInfo } from "./inbounds/constants";

export default function InboundsPage({
  refreshSignal,
  showWarnings,
}: {
  refreshSignal: number;
  showWarnings: boolean;
}) {
  const [tab, setTab] = useState<"inbounds" | "templates">("inbounds");
  const [list, setList] = useState<InboundList | null>(null);
  const [machines, setMachines] = useState<Record<number, MachineInfo>>({});
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [dialogItem, setDialogItem] = useState<InboundView | "new" | null>(
    null,
  );
  const [pendingDelete, setPendingDelete] = useState<InboundView | null>(
    null,
  );

  const refresh = useCallback(async () => {
    try {
      const data = await api.inbounds.list();
      setList(data);
      setLoadError(false);
      // 运行机器信息：本机取 local-machine，节点取节点列表 + 入站分配。
      // 机器信息拿不到时列表照常显示，对应格子显示“—”。
      void (async () => {
        try {
          const [lm, nodeList] = await Promise.all([
            api.localMachine(),
            api.nodes.list(),
          ]);
          const byId = new Map(nodeList.map((n) => [n.id, n]));
          const info: Record<number, MachineInfo> = {};
          await Promise.all(
            data.items.map(async (v) => {
              if (v.runLocal) {
                info[v.id] = {
                  name: lm.hostname || "本机",
                  ip: lm.ip || "—",
                  port: v.port,
                };
                return;
              }
              try {
                const res = await api.nodes.getInboundNodes(v.id);
                const first = res.nodes?.[0];
                const node = first ? byId.get(first.nodeId) : undefined;
                if (node) {
                  const host = (node.publicHost || node.address || "").split(
                    ":",
                  )[0];
                  info[v.id] = {
                    name: node.name || "—",
                    ip: host || "—",
                    port: first.port > 0 ? first.port : v.port,
                  };
                }
              } catch {
                // 节点信息拿不到时该行机器格留空，不影响列表本身。
              }
            }),
          );
          setMachines(info);
        } catch {
          // 机器信息整体拿不到时列表照常显示。
        }
      })();
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

  const confirmDelete = () => {
    if (!pendingDelete) return;
    const v = pendingDelete;
    setPendingDelete(null);
    setActionError(null);
    setBusyId(v.id);
    void (async () => {
      try {
        await api.inbounds.remove(v.id);
        await refresh();
        toast.success("入站已删除");
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : "删除失败，请重试",
        );
      } finally {
        setBusyId(null);
      }
    })();
  };

  const toggleEnabled = (v: InboundView) => {
    setActionError(null);
    setBusyId(v.id);
    void (async () => {
      try {
        await api.inbounds.setEnabled(v.id, !v.enabled);
        await refresh();
        toast.success(v.enabled ? "入站已停用" : "入站已启用");
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : "操作失败，请重试",
        );
      } finally {
        setBusyId(null);
      }
    })();
  };

  const tabSwitch = (
    <div className="inline-flex rounded-xl bg-muted p-1">
      {(
        [
          ["inbounds", "入站"],
          ["templates", "模板"],
        ] as const
      ).map(([key, label]) => (
        <button
          key={key}
          type="button"
          onClick={() => setTab(key)}
          className={cn(
            "h-7 rounded-lg px-3.5 text-sm transition-colors",
            tab === key
              ? "bg-background font-medium shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );

  if (!list) {
    return (
      <div className="space-y-4">
        {tabSwitch}
        {tab === "templates" ? (
          <TemplatesPanel refreshSignal={refreshSignal} />
        ) : (
          <p className="text-sm text-muted-foreground">
            {loadError ? "暂时无法获取入站数据，请稍后刷新重试" : "数据加载中…"}
          </p>
        )}
      </div>
    );
  }

  const outOfSync = list.items.filter(
    (v) =>
      v.status === "pending" ||
      v.status === "drifted" ||
      v.status === "not_listening",
  ).length;

  return (
    <div className="space-y-4">
      {tabSwitch}
      {tab === "templates" ? (
        <TemplatesPanel refreshSignal={refreshSignal} />
      ) : (
        <>
      {showWarnings && !list.runningAvailable && (
        <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-[13px] text-amber-700 dark:text-amber-300">
          无法读取核心的运行配置，本页仅显示面板记录，同步状态不可用。
        </div>
      )}
      {showWarnings && list.runningAvailable && outOfSync > 0 && (
        <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-[13px] text-amber-700 dark:text-amber-300">
          有 {outOfSync} 个入站与运行配置不一致 — 面板记录是期望状态，点顶栏的同步图标应用。
        </div>
      )}
      {list.extras.length > 0 && (
        <div className="rounded-[18px] border bg-card px-4 py-3 text-[13px] text-muted-foreground shadow-sm">
          核心正在运行、但面板尚未纳管的入站：
          {list.extras
            .map((e) => `${e.tag || e.type}（${e.type}:${e.port}）`)
            .join("、")}
        </div>
      )}

      <InboundListTable
        items={list.items}
        machines={machines}
        showWarnings={showWarnings}
        busyId={busyId}
        actionError={actionError}
        onCreate={() => setDialogItem("new")}
        onToggleEnabled={toggleEnabled}
        onEdit={(v) => setDialogItem(v)}
        onDelete={(v) => setPendingDelete(v)}
      />

      </>
      )}

      {dialogItem !== null && (
        <InboundFormDialog
          item={dialogItem === "new" ? null : dialogItem}
          showWarnings={showWarnings}
          onClose={() => setDialogItem(null)}
          onSaved={async () => {
            setDialogItem(null);
            await refresh();
          }}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title="删除入站"
        description={
          pendingDelete
            ? `确定要删除入站「${pendingDelete.name}」吗？此操作只删除面板记录，核心配置会在下次同步时移除该入站。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={confirmDelete}
      />
    </div>
  );
}
