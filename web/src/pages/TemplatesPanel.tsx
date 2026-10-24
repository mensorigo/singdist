import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
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
import {
  api,
  ApiError,
  type InboundView,
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
    <section className="rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
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
                  <div className="flex items-center gap-3">
                    <Button
                      onClick={() => setTplDialog(t)}
                      className="h-8 rounded-xl px-3"
                    >
                      编辑
                    </Button>
                    <Button
                      variant="destructive"
                      onClick={() => setPendingDelete(t)}
                      className="h-8 rounded-xl px-3"
                    >
                      删除
                    </Button>
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
  const [selected, setSelected] = useState<number[]>(item?.inboundIds ?? []);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const toggle = (id: number) =>
    setSelected((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id],
    );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const body = { name: name.trim(), inboundIds: selected };
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
      <DialogContent className="max-w-lg">
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
          <div className="space-y-2">
            <Label>包含的入站</Label>
            <div className="space-y-1.5">
              {inbounds.map((ib) => (
                <label
                  key={ib.id}
                  className="flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-muted/50"
                >
                  <input
                    type="checkbox"
                    checked={selected.includes(ib.id)}
                    onChange={() => toggle(ib.id)}
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
    </Dialog>
  );
}
