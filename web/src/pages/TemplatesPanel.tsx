import { useCallback, useEffect, useState } from "react";
import { Pencil, Trash } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { EnabledSwitch } from "@/components/enabled-switch";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  api,
  ApiError,
  type InboundView,
  type TemplateGroup,
  type TemplateView,
} from "@/lib/api";

const PROTOCOL_LABEL: Record<string, string> = {
  "vless-reality": "VLESS · Reality",
  hysteria2: "Hysteria2",
  tuic: "TUIC v5",
  shadowsocks: "Shadowsocks",
  anytls: "AnyTLS",
  trojan: "Trojan",
};

// Row action trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background.
const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// TemplatesPanel is the templates view inside the Inbounds page: a
// template is a named set of inbounds, so it is managed next to the
// inbounds themselves. Templates are owned by the backend templates
// module; subscriptions only reference them by id.
export default function TemplatesPanel({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [templates, setTemplates] = useState<TemplateView[] | null>(null);
  const [inbounds, setInbounds] = useState<InboundView[]>([]);
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [tplDialog, setTplDialog] = useState<TemplateView | "new" | null>(
    null,
  );
  const [pendingDelete, setPendingDelete] = useState<TemplateView | null>(
    null,
  );

  const refresh = useCallback(async () => {
    try {
      const [tpls, inb] = await Promise.all([
        api.templates.list(),
        api.inbounds.list(),
      ]);
      setTemplates(tpls);
      setInbounds(inb.items);
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

  if (!templates) {
    return (
      <p className="text-sm text-muted-foreground">
        {loadError ? "暂时无法获取模板数据，请稍后刷新重试" : "数据加载中…"}
      </p>
    );
  }

  const inboundName = (id: number) => {
    const ib = inbounds.find((x) => x.id === id);
    return ib ? `${ib.name}（${ib.port}）` : `#${id}`;
  };

  return (
    <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex items-center justify-between">
        <p className="text-base font-semibold">模板（入站集合）</p>
        <Button
          onClick={() => setTplDialog("new")}
          className="h-8 rounded-xl px-3"
        >
          新建模板
        </Button>
      </div>
      {actionError && (
        <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
      )}
      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[640px] text-sm">
          <thead>
            <tr className="border-b text-left text-xs text-muted-foreground">
              <th className="py-2 pr-3 font-medium">名称</th>
              <th className="py-2 pr-3 font-medium">包含入站</th>
              <th className="py-2 pr-3 font-medium">订阅数</th>
              <th className="py-2 font-medium">操作</th>
            </tr>
          </thead>
          <tbody>
            {templates.map((t) => (
              <tr key={t.id} className="border-b last:border-0">
                <td className="py-2.5 pr-3 font-medium">{t.name}</td>
                <td className="py-2.5 pr-3">
                  <div className="flex flex-wrap gap-1.5">
                    {t.inboundIds.map((id) => (
                      <span
                        key={id}
                        className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
                      >
                        {inboundName(id)}
                      </span>
                    ))}
                    {t.inboundIds.length === 0 && (
                      <span className="text-muted-foreground">（空）</span>
                    )}
                  </div>
                </td>
                <td className="py-2.5 pr-3 tabular-nums">
                  {t.subscriptionCount}
                </td>
                <td className="py-2.5">
                  <div className="flex items-center gap-1">
                    <button
                      type="button"
                      title="编辑"
                      aria-label="编辑"
                      onClick={() => setTplDialog(t)}
                      className={ICON_BTN_CLS}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      type="button"
                      title="删除"
                      aria-label="删除"
                      onClick={() => setPendingDelete(t)}
                      className={`${ICON_BTN_CLS}`}
                    >
                      <Trash className="h-4 w-4" />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {templates.length === 0 && (
              <tr>
                <td
                  colSpan={4}
                  className="py-6 text-center text-sm text-muted-foreground"
                >
                  暂无模板，模板是订阅要绑定的入站集合
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {tplDialog !== null && (
        <TemplateFormDialog
          item={tplDialog === "new" ? null : tplDialog}
          inbounds={inbounds}
          onClose={() => setTplDialog(null)}
          onSaved={async () => {
            setTplDialog(null);
            await refresh();
          }}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title="删除模板"
        description={
          pendingDelete
            ? `确定要删除模板「${pendingDelete.name}」吗？正在被订阅使用的模板无法删除。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={() => {
          if (!pendingDelete) return;
          const v = pendingDelete;
          setPendingDelete(null);
          setActionError(null);
          void (async () => {
            try {
              await api.templates.remove(v.id);
              await refresh();
              toast.success("模板已删除");
            } catch (err) {
              setActionError(
                err instanceof ApiError ? err.message : "删除失败，请重试",
              );
            }
          })();
        }}
      />
    </section>
  );
}

// --- template form: proxy groups -----------------------------------------------
// 分组已接线：模板保存时把分组写进后端，订阅生成时输出这些组。
// 默认组（Proxy / Auto / Fallback）与自定义组一样可编辑节点，
// 区别只是默认组不能删除、不能改名。
type GroupType = "select" | "url-test" | "fallback" | "load-balance";

const GROUP_TYPE_LABEL: Record<GroupType, string> = {
  select: "手动选择",
  "url-test": "自动选择",
  fallback: "故障转移",
  "load-balance": "负载均衡",
};

interface GroupDraft {
  id: string;
  name: string;
  type: GroupType;
  nodeIds: number[];
  enabled: boolean;
  url: string;
  interval: number;
  tolerance: number;
}

// 默认组：名字 + 类型与后端默认值一致的组，不能删除、不能改名，
// 但可以编辑节点和开关。自定义组可删可改名。
function isDefaultGroup(g: GroupDraft): boolean {
  return (
    (g.name === "Proxy" && g.type === "select") ||
    (g.name === "Auto" && g.type === "url-test") ||
    (g.name === "Fallback" && g.type === "fallback")
  );
}

function toDraft(g: TemplateGroup, fallbackId: string): GroupDraft {
  return {
    id: fallbackId,
    name: g.name,
    type: (g.type as GroupType) ?? "select",
    nodeIds: g.inboundIds ?? [],
    enabled: g.enabled !== false,
    url: g.url ?? "https://www.gstatic.com/generate_204",
    interval: g.interval ?? 300,
    tolerance: g.tolerance ?? 50,
  };
}

function toApiGroup(g: GroupDraft): TemplateGroup {
  const out: TemplateGroup = {
    name: g.name,
    type: g.type,
    inboundIds: g.nodeIds,
    enabled: g.enabled,
  };
  if (g.type !== "select") {
    out.url = g.url || "https://www.gstatic.com/generate_204";
    out.interval = g.interval > 0 ? g.interval : 300;
    out.tolerance = g.tolerance >= 0 ? g.tolerance : 50;
  }
  return out;
}

// --- template form --------------------------------------------------------------

function TemplateFormDialog({
  item,
  inbounds,
  onClose,
  onSaved,
}: {
  item: TemplateView | null;
  inbounds: InboundView[];
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const creating = item === null;
  const [name, setName] = useState(item?.name ?? "");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  // 第一层只管分组，不管节点：节点只在分组编辑（第二层）里挑。
  // 老模板以后端返回的有效分组初始化；新建模板的 3 个默认组
  // 节点为空，点编辑进去再加。
  const [groups, setGroups] = useState<GroupDraft[]>(() => {
    if (item !== null) {
      return (item.groups ?? []).map((g, i) => toDraft(g, `group-${i}`));
    }
    const empty: GroupDraft[] = [
      { id: "group-proxy", name: "Proxy", type: "select", nodeIds: [], enabled: true, url: "https://www.gstatic.com/generate_204", interval: 300, tolerance: 50 },
      { id: "group-auto", name: "Auto", type: "url-test", nodeIds: [], enabled: true, url: "https://www.gstatic.com/generate_204", interval: 300, tolerance: 50 },
      { id: "group-fallback", name: "Fallback", type: "fallback", nodeIds: [], enabled: true, url: "https://www.gstatic.com/generate_204", interval: 300, tolerance: 50 },
    ];
    return empty;
  });
  const [groupDialog, setGroupDialog] = useState<GroupDraft | "new" | null>(
    null,
  );
  const [pendingGroupDelete, setPendingGroupDelete] = useState<GroupDraft | null>(
    null,
  );
  const nodeChips = (ids: number[]) => {
    const nodes = inbounds.filter((ib) => ids.includes(ib.id));
    if (nodes.length === 0)
      return <span className="text-muted-foreground">（空）</span>;
    return (
      <div className="flex flex-wrap gap-1.5">
        {nodes.map((ib) => (
          <span
            key={ib.id}
            className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
          >
            {ib.name}（{ib.port}）
          </span>
        ))}
      </div>
    );
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      // 模板入站 = 所有分组节点的并集（含已关闭的组，关掉的组
      // 节点仍在全局节点列表里，只是没有分组引用它）。
      const union: number[] = [];
      for (const g of groups) {
        for (const id of g.nodeIds) {
          if (!union.includes(id)) union.push(id);
        }
      }
      const body = {
        name: name.trim(),
        inboundIds: union,
        groups: groups.map((g) => toApiGroup(g)),
      };
      if (creating) {
        await api.templates.create(body);
        toast.success("模板已创建");
      } else {
        await api.templates.update(item.id, body);
        toast.success("模板已保存");
      }
      await onSaved();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "保存失败，请重试");
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
      <DialogContent className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{creating ? "新建模板" : "编辑模板"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="tpl-name">名称</Label>
            <Input
              id="tpl-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：全部节点"
              required
            />
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <Label>分组</Label>
              <Button
                type="button"
                variant="outline"
                className="h-8 rounded-xl px-3"
                onClick={() => setGroupDialog("new")}
              >
                新建分组
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              第一层只创建和管理分组，不选节点；节点在分组编辑里挑。默认组不能删除、不能改名，但可以点编辑改节点和开关。模板入站 = 所有分组节点的并集。
            </p>
            <div className="grid gap-3 sm:grid-cols-3">
              {groups.map((g) => (
                <div
                  key={g.id}
                  className={`rounded-[18px] border px-4 py-3 ${isDefaultGroup(g) ? "bg-muted/30" : "bg-card"}`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium">{g.name}</span>
                    <div className="flex items-center gap-1">
                      <EnabledSwitch
                        enabled={g.enabled}
                        onToggle={() =>
                          setGroups((prev) =>
                            prev.map((x) =>
                              x.id === g.id
                                ? { ...x, enabled: !x.enabled }
                                : x,
                            ),
                          )
                        }
                      />
                      <button
                        type="button"
                        title="编辑分组"
                        aria-label="编辑分组"
                        onClick={() => setGroupDialog(g)}
                        className={ICON_BTN_CLS}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      {!isDefaultGroup(g) && (
                        <button
                          type="button"
                          title="删除分组"
                          aria-label="删除分组"
                          onClick={() => setPendingGroupDelete(g)}
                          className={ICON_BTN_CLS}
                        >
                          <Trash className="h-4 w-4" />
                        </button>
                      )}
                    </div>
                  </div>
                  <div className="mt-1.5 flex flex-wrap gap-1.5">
                    <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {GROUP_TYPE_LABEL[g.type]}
                    </span>
                    {isDefaultGroup(g) && (
                      <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                        系统默认
                      </span>
                    )}
                  </div>
                  <div className="mt-2.5">{nodeChips(g.nodeIds)}</div>
                </div>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">
              预览：
              {groups
                .filter((g) => g.enabled)
                .map((g) => `${g.name} ${g.nodeIds.length} 个节点`)
                .join(" · ") || "（全部关闭，订阅将没有分组）"}
            </p>
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
            <Button type="submit" disabled={submitting}>
              {submitting ? "保存中…" : creating ? "创建" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
      {groupDialog !== null && (
        <GroupFormDialog
          item={groupDialog === "new" ? null : groupDialog}
          inbounds={inbounds}
          onClose={() => setGroupDialog(null)}
          onSaved={(g) => {
            setGroups((prev) => {
              const found = prev.some((x) => x.id === g.id);
              return found
                ? prev.map((x) => (x.id === g.id ? g : x))
                : [...prev, g];
            });
            setGroupDialog(null);
          }}
        />
      )}
      <ConfirmDialog
        open={pendingGroupDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingGroupDelete(null);
        }}
        title="删除分组"
        description={
          pendingGroupDelete
            ? `确定要删除分组「${pendingGroupDelete.name}」吗？保存模板后订阅里就不再生成它。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={() => {
          if (!pendingGroupDelete) return;
          setGroups((prev) =>
            prev.filter((x) => x.id !== pendingGroupDelete.id),
          );
          setPendingGroupDelete(null);
        }}
      />
    </Dialog>
  );
}

function GroupFormDialog({
  item,
  inbounds,
  onClose,
  onSaved,
}: {
  item: GroupDraft | null;
  inbounds: InboundView[];
  onClose: () => void;
  onSaved: (g: GroupDraft) => void;
}) {
  const creating = item === null;
  const [gName, setGName] = useState(item?.name ?? "");
  const [gType, setGType] = useState<GroupType>(item?.type ?? "select");
  const [gNodes, setGNodes] = useState<number[]>(item?.nodeIds ?? []);
  const [gUrl, setGUrl] = useState(
    item?.url ?? "https://www.gstatic.com/generate_204",
  );
  const [gInterval, setGInterval] = useState(String(item?.interval ?? 300));
  const [gTolerance, setGTolerance] = useState(String(item?.tolerance ?? 50));
  const defaultGroup = item !== null && isDefaultGroup(item);
  const [gError, setGError] = useState<string | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (gName.trim() === "") {
      setGError("请填分组名字");
      return;
    }
    onSaved({
      id: item?.id ?? `local-${Date.now()}`,
      name: gName.trim(),
      type: gType,
      nodeIds: gNodes,
      enabled: item?.enabled ?? true,
      url: gUrl,
      interval: Number(gInterval) || 300,
      tolerance: Number(gTolerance) || 50,
    });
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{creating ? "新建分组" : "编辑分组"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="grp-name">
              分组名字{defaultGroup ? "（系统默认，不可改）" : ""}
            </Label>
            <Input
              id="grp-name"
              value={gName}
              onChange={(e) => setGName(e.target.value)}
              placeholder="例如：香港组"
              disabled={defaultGroup}
            />
          </div>
          <div className="space-y-2">
            <Label>类型</Label>
            <Select
              value={gType}
              onValueChange={(v) => setGType(v as GroupType)}
              disabled={defaultGroup}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(GROUP_TYPE_LABEL) as GroupType[]).map((t) => (
                  <SelectItem key={t} value={t}>
                    {GROUP_TYPE_LABEL[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>包含的节点</Label>
              <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={
                    inbounds.length > 0 &&
                    inbounds.every((ib) => gNodes.includes(ib.id))
                  }
                  onChange={(e) =>
                    setGNodes(
                      e.target.checked ? inbounds.map((ib) => ib.id) : [],
                    )
                  }
                  className="h-4 w-4 accent-primary"
                />
                全选
              </label>
            </div>
            <div className="max-h-[220px] space-y-1.5 overflow-y-auto pr-1">
              {inbounds.map((ib) => (
                <label
                  key={ib.id}
                  className="flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-muted/50"
                >
                  <input
                    type="checkbox"
                    checked={gNodes.includes(ib.id)}
                    onChange={() =>
                      setGNodes((prev) =>
                        prev.includes(ib.id)
                          ? prev.filter((x) => x !== ib.id)
                          : [...prev, ib.id],
                      )
                    }
                    className="h-4 w-4 accent-primary"
                  />
                  <span className="font-medium">{ib.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {PROTOCOL_LABEL[ib.protocol] ?? ib.protocol} · 端口{" "}
                    {ib.port}
                    {!ib.enabled && " · 已停用"}
                  </span>
                </label>
              ))}
              {inbounds.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  暂无入站，请先在「入站」视图创建
                </p>
              )}
            </div>
          </div>
          <details className="space-y-2">
            <summary className="cursor-pointer text-sm text-muted-foreground">
              高级（测速地址、间隔、容差）
            </summary>
            <div className="space-y-2 pt-1">
              <div className="space-y-2">
                <Label htmlFor="grp-url">测速地址</Label>
                <Input
                  id="grp-url"
                  value={gUrl}
                  onChange={(e) => setGUrl(e.target.value)}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <Label htmlFor="grp-interval">间隔（秒）</Label>
                  <Input
                    id="grp-interval"
                    type="number"
                    value={gInterval}
                    onChange={(e) => setGInterval(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="grp-tolerance">容差（毫秒）</Label>
                  <Input
                    id="grp-tolerance"
                    type="number"
                    value={gTolerance}
                    onChange={(e) => setGTolerance(e.target.value)}
                  />
                </div>
              </div>
            </div>
          </details>
          {gError && (
            <p className="text-[13px] text-destructive">{gError}</p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button type="submit">{creating ? "添加" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
