import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Download,
  Ellipsis,
  Pause,
  Pencil,
  Play,
  RefreshCw,
  Trash2,
  Unlock,
  Upload,
} from "lucide-react";
import {
  api,
  ApiError,
  type ImportCandidate,
  type InboundView,
  type NodeView,
} from "@/lib/api";

function fmtTime(ts: number): string {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString("zh-CN", { hour12: false });
}

function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

function fmtMem(n: number): string {
  return fmtBytes(n);
}

// Card/menu trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background. Disabled triggers add
// opacity-50 for affordance; the baseline triggers are never disabled
// so they do not need it.
const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// A bare IP/host without a scheme or port gets the default agent port
// before sending, so the backend always sees host:port. Inputs that
// already carry a scheme or a port (and bare IPv6) pass through
// untouched; the backend rejects those it cannot parse, as today.
function normalizeServerIP(raw: string): string {
  const v = raw.trim();
  if (v === "") return v;
  if (/^https?:\/\//i.test(v)) return v;
  if (v.startsWith("[")) {
    return /\]:\d+$/.test(v) ? v : `${v}:32435`;
  }
  const colons = (v.match(/:/g) ?? []).length;
  if (colons === 0) return `${v}:32435`;
  if (colons === 1) {
    const after = v.split(":").pop() ?? "";
    if (/^\d+$/.test(after)) return v;
    if (after === "") return `${v}32435`;
    return v;
  }
  return v;
}

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
          <Card
            key={n.id}
            className={`flex h-full flex-col ${n.paused ? "opacity-70" : ""}`}
          >
            <CardHeader className="space-y-2 pb-3">
              <div className="flex flex-row items-center justify-between gap-2 space-y-0">
                <CardTitle className="min-w-0 truncate text-[24px]">
                  {n.name}
                </CardTitle>
                <span className="flex shrink-0 items-center gap-1">
                  {n.syncStatus === "synced" && (
                    <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300">
                      已同步
                    </span>
                  )}
                  <span
                    className={
                      n.paused
                        ? "rounded-full bg-amber-500/15 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300"
                        : n.online
                          ? "rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300"
                          : "rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                    }
                  >
                    {n.paused ? "已暂停" : n.online ? "在线" : "离线"}
                  </span>
                </span>
              </div>
              <p className="text-xs text-muted-foreground">
                {n.lastVersion ? `agent ${n.lastVersion} · ` : ""}
                {n.lastInboundCount} 个入站
              </p>
            </CardHeader>
            <CardContent className="flex-1 space-y-3 text-sm">
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">服务器IP</dt>
                <dd className="min-w-0 truncate text-foreground">
                  {n.address}
                </dd>
                {n.publicHost && (
                  <>
                    <dt className="text-muted-foreground">对外地址</dt>
                    <dd className="min-w-0 truncate text-foreground">
                      {n.publicHost}
                    </dd>
                  </>
                )}
              </dl>
              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">CPU 使用率</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available
                      ? `${n.agentSystem.cpuPct.toFixed(2)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    1 分钟负载{" "}
                    {n.agentSystem?.available
                      ? n.agentSystem.load1.toFixed(2)
                      : "—"}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">内存占用</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available
                      ? `${n.agentSystem.memPct.toFixed(0)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentSystem?.available
                      ? `${fmtMem(n.agentSystem.memUsed)} / ${fmtMem(n.agentSystem.memTotal)}`
                      : "暂无数据"}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">硬盘占用</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available && n.agentSystem.diskTotal > 0
                      ? `${n.agentSystem.diskPct.toFixed(2)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentSystem?.available && n.agentSystem.diskTotal > 0
                      ? `${fmtMem(n.agentSystem.diskUsed)} / ${fmtMem(n.agentSystem.diskTotal)}`
                      : "暂无数据"}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">总流量</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentTraffic
                      ? fmtBytes(n.agentTraffic.up + n.agentTraffic.down)
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentTraffic
                      ? `上行 ${fmtBytes(n.agentTraffic.up)} · 下行 ${fmtBytes(n.agentTraffic.down)}`
                      : "暂无数据"}
                  </p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                上次推送{" "}
                {n.lastPushResult
                  ? `${n.lastPushResult}（${fmtTime(n.lastPushAt)}）`
                  : "—"}
              </p>
              {n.agentStatusAt === 0 && (
                <p className="text-[13px] text-muted-foreground">
                  暂无节点状态快照，等探查或点刷新状态
                </p>
              )}
              {(n.syncStatus === "pending" ||
                n.syncStatus === "mismatch") &&
                n.syncDetail && (
                  <p className="text-[13px] text-amber-700 dark:text-amber-300">
                    {n.syncDetail}
                  </p>
                )}
              {n.lastError && (
                <p className="text-[13px] text-destructive">{n.lastError}</p>
              )}
              {n.paused && n.pausePending && (
                <p className="text-[13px] text-amber-700 dark:text-amber-300">
                  暂停还没落到节点，节点入站还在跑
                </p>
              )}
            </CardContent>
            <CardFooter className="mt-auto flex flex-wrap justify-end gap-1">
              <button
                type="button"
                title="拉取"
                aria-label="拉取"
                onClick={() => runImportScan(n, false)}
                disabled={acting !== null || n.paused}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Download className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? "推送（推空，停掉节点入站）" : "推送"}
                aria-label="推送"
                onClick={() => handlePush(n)}
                disabled={acting !== null}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Upload className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? "启用" : "暂停"}
                aria-label={n.paused ? "启用" : "暂停"}
                onClick={() => handleTogglePause(n)}
                disabled={acting !== null}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                {n.paused ? (
                  <Play className="h-4 w-4" />
                ) : (
                  <Pause className="h-4 w-4" />
                )}
              </button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button
                    type="button"
                    title="更多"
                    aria-label="更多"
                    disabled={acting !== null}
                    className={`${ICON_BTN_CLS} disabled:opacity-50`}
                  >
                    <Ellipsis className="h-4 w-4" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-32">
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => setDialog(n)}
                  >
                    <Pencil className="mr-1.5 h-4 w-4" />
                    编辑
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => handleProbe(n)}
                  >
                    <RefreshCw className="mr-1.5 h-4 w-4" />
                    刷新状态
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => handleClearBlock(n)}
                  >
                    <Unlock className="mr-1.5 h-4 w-4" />
                    清除阻塞并重推
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => setDeleteTarget(n)}
                  >
                    <Trash2 className="mr-1.5 h-4 w-4" />
                    删除
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </CardFooter>
          </Card>
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

function NodeImportDialog({
  node,
  candidates,
  onClose,
  onImported,
}: {
  node: NodeView;
  candidates: ImportCandidate[];
  onClose: () => void;
  onImported: () => Promise<void>;
}) {
  const importable = candidates.filter((c) => !c.disabledReason);
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(importable.map((c) => c.name)),
  );
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const handleConfirm = async () => {
    setSubmitting(true);
    setFormError(null);
    try {
      const res = await api.nodes.importInbounds(node.id, [...selected]);
      if (res.created.length > 0) {
        toast.success(
          `已导入 ${res.created.length} 个入站：${res.created.map((c) => c.name).join("、")}`,
        );
      }
      for (const s of res.skipped) {
        toast.warning(`${s.name} 未导入：${s.reason}`);
      }
      if (res.created.length === 0 && res.skipped.length === 0) {
        toast("没有导入任何入站");
      }
      await onImported();
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : "导入失败，请重试",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>从 {node.name} 导入入站</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          导入的入站不带用户，需加入订阅模板后才会有用户。导入后只跑在这个节点上。
        </p>
        <div className="space-y-2">
          {candidates.map((c) => (
            <label key={c.name} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={selected.has(c.name)}
                disabled={!!c.disabledReason}
                onChange={() => toggle(c.name)}
              />
              <span>{c.name}</span>
              <span className="text-muted-foreground">
                {c.protocol} · {c.port}
              </span>
              {c.disabledReason && (
                <span className="text-[13px] text-destructive">
                  {c.disabledReason}
                </span>
              )}
            </label>
          ))}
        </div>
        {formError && (
          <p className="text-[13px] text-destructive">{formError}</p>
        )}
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={submitting}
          >
            取消
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={submitting || selected.size === 0}
          >
            {submitting ? "导入中…" : "导入"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function NodeFormDialog({
  item,
  onClose,
  onSaved,
  onCreated,
}: {
  item: NodeView | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onCreated?: (created: NodeView) => void;
}) {
  const creating = item === null;
  const [name, setName] = useState(item?.name ?? "");
  const [address, setAddress] = useState(item?.address ?? "");
  const [pairingCode, setPairingCode] = useState("");
  const [pairingLock, setPairingLock] = useState(0);
  const [pairingRequested, setPairingRequested] = useState(false);
  const [publicHost, setPublicHost] = useState(item?.publicHost ?? "");
  const [assignedNames, setAssignedNames] = useState<string[] | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (creating || !item) return;
    let cancelled = false;
    (async () => {
      try {
        const [detail, list] = await Promise.all([
          api.nodes.get(item.id),
          api.inbounds.list(),
        ]);
        if (cancelled) return;
        const byId = new Map<number, InboundView>();
        for (const ib of list.items) byId.set(ib.id, ib);
        setAssignedNames(
          detail.inboundIds.map((id) => byId.get(id)?.name ?? `#${id}`),
        );
      } catch {
        if (!cancelled) setAssignedNames([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [creating, item]);

  // Countdown for the pairing-request lock (agent enforces 60s;
  // the frontend mirrors it so the button cannot be spammed).
  useEffect(() => {
    if (pairingLock <= 0) return;
    const timer = setTimeout(() => setPairingLock((v) => Math.max(0, v - 1)), 1000);
    return () => clearTimeout(timer);
  }, [pairingLock]);

  const handleRequestPairingCode = async () => {
    const addr = normalizeServerIP(address);
    if (addr === "") {
      setFormError("请先填服务器IP");
      return;
    }
    setFormError(null);
    try {
      const res = await api.nodes.requestPairingCode(addr);
      setPairingRequested(true);
      setPairingLock(res.retry_after > 0 ? res.retry_after : 60);
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : "请求生成配对码失败",
      );
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      let created: NodeView | null = null;
      // The pairing code is the only credential in this form: the
      // panel exchanges it for a long-term token and stores that
      // itself, so no token is ever typed here. Creating requires a
      // code; editing with an empty code sends no credential field
      // at all, and the backend keeps the existing token.
      const code = pairingCode.trim();
      if (creating && code === "") {
        setFormError("请先填配对码");
        setSubmitting(false);
        return;
      }
      if (creating) {
        created = await api.nodes.create({
          name: name.trim(),
          address: normalizeServerIP(address),
          pairing_code: code,
          publicHost: publicHost.trim() || undefined,
        });
        toast.success("节点已添加（5 秒内开始首次推送和状态探测）");
        if (created.warning) {
          toast.warning(created.warning);
        }
      } else {
        await api.nodes.update(item.id, {
          name: name.trim(),
          address: normalizeServerIP(address),
          pairing_code: code || undefined,
          publicHost: publicHost.trim(),
        });
        toast.success("节点已保存");
      }
      await onSaved();
      // Creation-time probe succeeded (no warning): auto-scan the
      // node's core once for importable inbounds. The scan itself
      // stays silent unless something can actually be imported.
      if (creating && created && !created.warning) {
        onCreated?.(created);
      }
    } catch (err) {
      setFormError(
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "保存失败，请重试",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{creating ? "添加节点" : "编辑节点"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="node-name">备注</Label>
            <Input
              id="node-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="主机备注"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-address">服务器IP</Label>
            <Input
              id="node-address"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="IP地址"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-pairing">配对码</Label>
            <div className="flex gap-2">
              <Input
                id="node-pairing"
                value={pairingCode}
                onChange={(e) =>
                  setPairingCode(
                    e.target.value.replace(/[^A-Za-z0-9]/g, "").slice(0, 8),
                  )
                }
                placeholder={
                  creating
                    ? "8 位配对码，子服务器终端查看"
                    : "留空保持不变，填了则重新配对"
                }
                maxLength={8}
                className="flex-1"
              />
              <Button
                type="button"
                variant="outline"
                onClick={handleRequestPairingCode}
                disabled={pairingLock > 0 || submitting}
              >
                {pairingLock > 0 ? `发送请求（${pairingLock}s）` : "发送请求"}
              </Button>
            </div>
            {pairingRequested && (
              <p className="text-sm text-muted-foreground">
                已请求生成，请去子服务器终端查看配对码，5分钟内有效。
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-public">对外地址（可选）</Label>
            <Input
              id="node-public"
              value={publicHost}
              onChange={(e) => setPublicHost(e.target.value)}
              placeholder="节点对外地址显示，默认使用IP地址，可以使用域名"
            />
          </div>
          {!creating && assignedNames !== null && (
            <p className="text-sm text-muted-foreground">
              已分配入站：
              {assignedNames.length > 0 ? assignedNames.join("、") : "无（仅本地）"}
              。分配在入站编辑里改。
            </p>
          )}
          {formError && (
            <p className="text-[13px] text-destructive">{formError}</p>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={submitting}
            >
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? "保存中…" : creating ? "添加" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
