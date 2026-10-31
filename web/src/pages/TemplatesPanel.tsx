import { useCallback, useEffect, useState } from "react";
import { Pencil, Trash } from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { TemplateFormDialog } from "./templates/TemplateFormDialog";
import { ICON_BTN_CLS } from "./templates/shared";
import { Button } from "@/components/ui/button";
import {
  api,
  ApiError,
  type InboundView,
  type TemplateView,
} from "@/lib/api";

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

