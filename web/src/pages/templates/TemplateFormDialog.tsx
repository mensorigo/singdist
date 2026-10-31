import { useState } from "react";
import { Pencil, Trash } from "lucide-react";
import { toast } from "sonner";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EnabledSwitch } from "@/components/enabled-switch";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, ApiError, type InboundView, type TemplateView } from "@/lib/api";
import { ICON_BTN_CLS } from "./shared";
import {
  GROUP_TYPE_LABEL,
  isDefaultGroup,
  toApiGroup,
  toDraft,
  type GroupDraft,
} from "./group";
import { GroupFormDialog } from "./GroupFormDialog";

// --- template form --------------------------------------------------------------

export function TemplateFormDialog({
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

