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
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  api,
  ApiError,
  type InboundView,
  type NodeView,
} from "@/lib/api";

function fmtTime(ts: number): string {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString("zh-CN", { hour12: false });
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
      toast.success(
        `${n.name} 在线，${count} 个入站${res.status.version ? `，agent ${res.status.version}` : ""}`,
      );
      await load();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "状态查询失败");
      await load();
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
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">节点</h2>
        <Button onClick={() => setDialog("new")}>添加节点</Button>
      </div>
      <p className="text-sm text-muted-foreground">
        节点是装了独立 agent 的服务器。中央面板把入站定义推过去，节点本地生成并应用到自己的核心；订阅里会同时出现本地和各节点的入口。
      </p>
      {loadError && <p className="text-[13px] text-destructive">{loadError}</p>}
      {nodes.length === 0 && !loadError && (
        <p className="text-sm text-muted-foreground">
          还没有节点。先在要接管的服务器上装好 agent，再点「添加节点」填地址和 token。
        </p>
      )}
      <div className="grid gap-4 md:grid-cols-2">
        {nodes.map((n) => (
          <Card key={n.id}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0">
              <CardTitle className="text-base">{n.name}</CardTitle>
              <span
                className={
                  n.online
                    ? "rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300"
                    : "rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                }
              >
                {n.online ? "在线" : "离线"}
              </span>
            </CardHeader>
            <CardContent className="space-y-1 text-sm">
              <p className="text-muted-foreground">
                管理地址：<span className="text-foreground">{n.address}</span>
              </p>
              {n.publicHost && (
                <p className="text-muted-foreground">
                  对外地址：<span className="text-foreground">{n.publicHost}</span>
                </p>
              )}
              <p className="text-muted-foreground">
                入站：<span className="text-foreground">{n.lastInboundCount} 个</span>
                {n.lastVersion && (
                  <>
                    {" · "}agent{" "}
                    <span className="text-foreground">{n.lastVersion}</span>
                  </>
                )}
              </p>
              <p className="text-muted-foreground">
                上次推送：
                <span className="text-foreground">
                  {n.lastPushResult ? `${n.lastPushResult}（${fmtTime(n.lastPushAt)}）` : "—"}
                </span>
              </p>
              {n.lastError && (
                <p className="text-[13px] text-destructive">{n.lastError}</p>
              )}
            </CardContent>
            <CardFooter className="flex flex-wrap gap-2">
              <Button
                size="sm"
                onClick={() => handlePush(n)}
                disabled={acting !== null}
              >
                推送
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setDialog(n)}
                disabled={acting !== null}
              >
                编辑
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button size="sm" variant="outline" disabled={acting !== null}>
                    更多
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent>
                  <DropdownMenuItem onClick={() => handleProbe(n)}>
                    刷新状态
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleClearBlock(n)}>
                    清除阻塞并重推
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button
                size="sm"
                variant="destructive"
                onClick={() => setDeleteTarget(n)}
                disabled={acting !== null}
              >
                删除
              </Button>
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
        />
      )}
      {deleteTarget !== null && (
        <ConfirmDialog
          open
          onOpenChange={(open) => {
            if (!open) setDeleteTarget(null);
          }}
          title="删除节点"
          description={`先向 ${deleteTarget.name} 推空配置（收回中央管理的入站），再删除记录。如果节点当时连不上，记录仍会删除，但节点可能继续跑旧配置，需要手动停掉它的 agent。确定删除？`}
          confirmLabel="删除"
          destructive
          onConfirm={handleDelete}
        />
      )}
    </div>
  );
}

function NodeFormDialog({
  item,
  onClose,
  onSaved,
}: {
  item: NodeView | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const creating = item === null;
  const [name, setName] = useState(item?.name ?? "");
  const [address, setAddress] = useState(item?.address ?? "");
  const [token, setToken] = useState("");
  const [publicHost, setPublicHost] = useState(item?.publicHost ?? "");
  const [usageRatio, setUsageRatio] = useState(
    item ? String(item.usageRatio) : "1",
  );
  const [addToAll, setAddToAll] = useState(false);
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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const ratio = Number(usageRatio);
      if (!Number.isFinite(ratio) || ratio <= 0) {
        throw new Error("使用系数必须大于 0");
      }
      if (creating) {
        await api.nodes.create({
          name: name.trim(),
          address: address.trim(),
          token: token.trim(),
          publicHost: publicHost.trim() || undefined,
          usageRatio: ratio,
          addToAllInbounds: addToAll,
        });
        toast.success("节点已添加（5 秒内开始首次推送和状态探测）");
      } else {
        await api.nodes.update(item.id, {
          name: name.trim(),
          address: address.trim(),
          token: token.trim() || undefined,
          publicHost: publicHost.trim(),
          usageRatio: ratio,
        });
        toast.success("节点已保存");
      }
      await onSaved();
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
            <Label htmlFor="node-name">名字</Label>
            <Input
              id="node-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="比如：东京、法兰克福"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-address">管理地址</Label>
            <Input
              id="node-address"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="203.0.113.7:32435，默认 https；测试用 http:// 前缀"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-token">Token</Label>
            <Input
              id="node-token"
              type="password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={
                creating ? "agent 启动时设置的 token" : "留空保持不变"
              }
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-public">对外地址（可选）</Label>
            <Input
              id="node-public"
              value={publicHost}
              onChange={(e) => setPublicHost(e.target.value)}
              placeholder="订阅里给客户端的地址，留空就用管理地址的主机部分"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-ratio">使用系数（以后流量加权用，默认 1）</Label>
            <Input
              id="node-ratio"
              type="number"
              min="0"
              step="any"
              value={usageRatio}
              onChange={(e) => setUsageRatio(e.target.value)}
            />
          </div>
          {creating && (
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={addToAll}
                onChange={(e) => setAddToAll(e.target.checked)}
              />
              作为所有入站的主机（新节点立即获得全部入站）
            </label>
          )}
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
