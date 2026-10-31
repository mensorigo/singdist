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
import i18n from "@/lib/i18n";

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
                  name: lm.hostname || i18n.t('inbounds.k001'),
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
        toast.success(i18n.t('inbounds.k002'));
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : i18n.t('inbounds.k003'),
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
        toast.success(v.enabled ? i18n.t('inbounds.k004') : i18n.t('inbounds.k005'));
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : i18n.t('inbounds.k006'),
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
          ["inbounds", i18n.t('inbounds.k007')],
          ["templates", i18n.t('inbounds.k008')],
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
            {loadError ? i18n.t('inbounds.k009') : i18n.t('inbounds.k010')}
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
          {i18n.t('inbounds.k060')}
        </div>
      )}
      {showWarnings && list.runningAvailable && outOfSync > 0 && (
        <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-[13px] text-amber-700 dark:text-amber-300">
          {i18n.t('inbounds.k061', { outOfSync: outOfSync })}
        </div>
      )}
      {list.extras.length > 0 && (
        <div className="rounded-[18px] border bg-card px-4 py-3 text-[13px] text-muted-foreground shadow-sm">
          {i18n.t('inbounds.k062')}
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
        title={i18n.t('inbounds.k011')}
        description={
          pendingDelete
            ? i18n.t('inbounds.k012', { value: pendingDelete.name })
            : undefined
        }
        confirmLabel={i18n.t('inbounds.k013')}
        destructive
        onConfirm={confirmDelete}
      />
    </div>
  );
}
