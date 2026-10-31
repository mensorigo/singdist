import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  api,
  ApiError,
  type ImportCandidate,
  type NodeView,
} from "@/lib/api";
import { NodeCard } from "./nodes/NodeCard";
import { NodeFormDialog } from "./nodes/NodeFormDialog";
import { NodeImportDialog } from "./nodes/NodeImportDialog";
import { fmtBytes } from "./nodes/format";
import i18n from "@/lib/i18n";

export default function NodesPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [nodes, setNodes] = useState<NodeView[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialog, setDialog] = useState<NodeView | "new" | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<NodeView | null>(null);
  const [acting, setActing] = useState<string | null>(null);
  const [importTarget, setImportTarget] = useState<{
    node: NodeView;
    candidates: ImportCandidate[];
  } | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await api.nodes.list();
      setNodes(list);
      setLoadError(null);
    } catch (err) {
      setLoadError(err instanceof ApiError ? err.message : i18n.t('nodes.k001'));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load, refreshSignal]);

  const handlePush = async (n: NodeView) => {
    setActing(`push-${n.id}`);
    try {
      const res = await api.nodes.push(n.id);
      if (res.started) {
        toast.success(i18n.t('nodes.k002', { value: n.name }));
      } else {
        toast(i18n.t('nodes.k003', { value: n.name }));
      }
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k004'));
    } finally {
      setActing(null);
    }
  };

  const handleProbe = async (n: NodeView) => {
    setActing(`probe-${n.id}`);
    try {
      const res = await api.nodes.probeStatus(n.id);
      const count = res.status.inbounds.length;
      const syncText =
        res.node.syncStatus === "synced"
          ? i18n.t('nodes.k005')
          : res.node.syncDetail
            ? `，${res.node.syncDetail}`
            : "";
      toast.success(
        i18n.t('nodes.k006', { name: n.name, count, version: res.status.version ? `，agent ${res.status.version}` : "", sync: syncText }),
      );
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k007'));
      await load();
    } finally {
      setActing(null);
    }
  };

  // Scan a node's core for importable inbounds. Auto-scan (after node
  // creation) stays silent unless something can actually be imported;
  // manual sync always reports back (dialog or toast).
  const runImportScan = async (n: NodeView, silent: boolean) => {
    setActing(`import-${n.id}`);
    try {
      const res = await api.nodes.importScan(n.id);
      const importable = res.candidates.filter((c) => !c.disabledReason);
      if (silent) {
        if (importable.length > 0) {
          setImportTarget({ node: n, candidates: res.candidates });
        }
      } else if (res.candidates.length > 0) {
        setImportTarget({ node: n, candidates: res.candidates });
      } else {
        toast(i18n.t('nodes.k008'));
      }
    } catch (err) {
      if (!silent) {
        toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k009'));
      }
    } finally {
      setActing(null);
    }
  };

  const handleClearBlock = async (n: NodeView) => {
    setActing(`clear-${n.id}`);
    try {
      const res = await api.nodes.clearBlock(n.id);
      if (res.cleared) {
        toast.success(i18n.t('nodes.k010', { value: n.name }));
      } else {
        toast(i18n.t('nodes.k011', { value: n.name }));
      }
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k012'));
    } finally {
      setActing(null);
    }
  };

  // Pause is panel-side only: the node keeps running, its record and
  // assignments stay, and enabling restores it in subscriptions.
  const handleTogglePause = async (n: NodeView) => {
    setActing(`pause-${n.id}`);
    try {
      await api.nodes.update(n.id, { paused: !n.paused });
      toast.success(i18n.t('nodes.k013', { name: n.name, action: n.paused ? i18n.t('nodes.k014') : i18n.t('nodes.k015') }));
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k016'));
    } finally {
      setActing(null);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      const res = await api.nodes.remove(deleteTarget.id);
      if (res.warning) {
        toast.warning(res.warning);
      } else {
        toast.success(i18n.t('nodes.k017', { value: deleteTarget.name }));
      }
      setDeleteTarget(null);
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : i18n.t('nodes.k018'));
    }
  };

  return (
    <div className="space-y-4">
      {/* Overview on the left, create button on the right, both
          inside one card so the row lines up with the cards
          below instead of floating bare. */}
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-[18px] border bg-card px-4 py-3 shadow-sm">
        <div className="flex flex-wrap items-center gap-6">
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('nodes.k019')}</p>
            <p className="text-base font-semibold">{nodes.length}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('nodes.k020')}</p>
            <p className="text-base font-semibold">
              {nodes.filter((n) => n.online && !n.paused).length}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('nodes.k021')}</p>
            <p className="text-base font-semibold">
              {nodes.filter((n) => n.paused).length}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">{i18n.t('nodes.k022')}</p>
            <p className="text-base font-semibold">
              {nodes.some((n) => n.agentTraffic)
                ? fmtBytes(
                    nodes.reduce(
                      (s, n) =>
                        s +
                        (n.agentTraffic
                          ? n.agentTraffic.up + n.agentTraffic.down
                          : 0),
                      0,
                    ),
                  )
                : "—"}
            </p>
          </div>
        </div>
        <Button
          onClick={() => setDialog("new")}
          className="h-8 rounded-xl px-3"
        >
          {i18n.t('nodes.k072')}
        </Button>
      </div>
      <div className="rounded-[18px] border bg-card px-4 py-3 text-[13px] text-muted-foreground shadow-sm">
        {i18n.t('nodes.k073')}
      </div>
      {loadError && <p className="text-[13px] text-destructive">{loadError}</p>}
      {nodes.length === 0 && !loadError && (
        <div className="rounded-[18px] border bg-card px-6 py-10 text-center text-sm text-muted-foreground shadow-sm">
          {i18n.t('nodes.k074')}
        </div>
      )}
      <div className="grid items-stretch gap-5 sm:grid-cols-2 xl:grid-cols-3">
        {nodes.map((n) => (
          <NodeCard
            key={n.id}
            n={n}
            acting={acting}
            onImportScan={(node) => runImportScan(node, false)}
            onPush={handlePush}
            onTogglePause={handleTogglePause}
            onEdit={(node) => setDialog(node)}
            onProbe={handleProbe}
            onClearBlock={handleClearBlock}
            onDelete={(node) => setDeleteTarget(node)}
          />
        ))}
      </div>


      {dialog !== null && (
        <NodeFormDialog
          item={dialog === "new" ? null : dialog}
          onClose={() => setDialog(null)}
          onSaved={async () => {
            setDialog(null);
            await load();
          }}
          onCreated={(created) => runImportScan(created, true)}
        />
      )}
      {importTarget !== null && (
        <NodeImportDialog
          node={importTarget.node}
          candidates={importTarget.candidates}
          onClose={() => setImportTarget(null)}
          onImported={async () => {
            setImportTarget(null);
            await load();
          }}
        />
      )}
      {deleteTarget !== null && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null);
          }}
          title={i18n.t('nodes.k023')}
          description={i18n.t('nodes.k024', { value: deleteTarget.name })}
          confirmLabel={i18n.t('nodes.k025')}
          destructive
          onConfirm={handleDelete}
        />
      )}
    </div>
  );
}
