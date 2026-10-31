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
      setLoadError(err instanceof ApiError ? err.message : "加载失败");
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
        toast.success(`已触发推送：${n.name}`);
      } else {
        toast(`已有推送在进行：${n.name}`);
      }
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "触发推送失败");
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
          ? "，已同步"
          : res.node.syncDetail
            ? `，${res.node.syncDetail}`
            : "";
      toast.success(
        `${n.name} 在线，${count} 个入站${res.status.version ? `，agent ${res.status.version}` : ""}${syncText}`,
      );
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "状态查询失败");
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
        toast("该节点没有可导入的入站");
      }
    } catch (err) {
      if (!silent) {
        toast.error(err instanceof ApiError ? err.message : "扫描失败");
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
        toast.success(`${n.name} 阻塞已清除，已触发重推`);
      } else {
        toast(`${n.name} 当前没有阻塞`);
      }
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "清除阻塞失败");
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
      toast.success(`${n.name}已${n.paused ? "启用" : "暂停"}`);
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "切换失败");
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
        toast.success(`节点已删除：${deleteTarget.name}`);
      }
      setDeleteTarget(null);
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "删除失败");
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
            <p className="text-xs text-muted-foreground">节点总数</p>
            <p className="text-base font-semibold">{nodes.length}</p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">在线</p>
            <p className="text-base font-semibold">
              {nodes.filter((n) => n.online && !n.paused).length}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">已暂停</p>
            <p className="text-base font-semibold">
              {nodes.filter((n) => n.paused).length}
            </p>
          </div>
          <div>
            <p className="text-xs text-muted-foreground">节点总流量</p>
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
          添加节点
        </Button>
      </div>
      <div className="rounded-[18px] border bg-card px-4 py-3 text-[13px] text-muted-foreground shadow-sm">
        节点是装了独立 agent 的服务器。中央面板把入站定义推过去，节点本地生成并应用到自己的核心；订阅里会同时出现本地和各节点的入口。
      </div>
      {loadError && <p className="text-[13px] text-destructive">{loadError}</p>}
      {nodes.length === 0 && !loadError && (
        <div className="rounded-[18px] border bg-card px-6 py-10 text-center text-sm text-muted-foreground shadow-sm">
          还没有节点。先在要接管的服务器上装好 agent，再点「添加节点」填地址和配对码。
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
          title="删除节点"
          description={`删除 ${deleteTarget.name} 后，面板上的节点记录、分派、只跑在这台服务器上的入站（及模板里的引用）都会删掉，订阅里永久没它，要回来得重新加；但这台服务器自己跑的入站不受影响、继续跑。确定删除？`}
          confirmLabel="删除"
          destructive
          onConfirm={handleDelete}
        />
      )}
    </div>
  );
}
