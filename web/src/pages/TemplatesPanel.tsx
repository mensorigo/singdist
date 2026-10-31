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
import i18n from "@/lib/i18n";

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
        {loadError ? i18n.t('templates.k001') : i18n.t('templates.k002')}
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
        <p className="text-base font-semibold">{i18n.t('templates.k003')}</p>
        <Button
          onClick={() => setTplDialog("new")}
          className="h-8 rounded-xl px-3"
        >
          {i18n.t('templates.k046')}
        </Button>
      </div>
      {actionError && (
        <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
      )}
      <div className="mt-3 overflow-x-auto">
        <table className="w-full min-w-[640px] text-sm">
          <thead>
            <tr className="border-b text-left text-xs text-muted-foreground">
              <th className="py-2 pr-3 font-medium">{i18n.t('templates.k004')}</th>
              <th className="py-2 pr-3 font-medium">{i18n.t('templates.k005')}</th>
              <th className="py-2 pr-3 font-medium">{i18n.t('templates.k006')}</th>
              <th className="py-2 font-medium">{i18n.t('templates.k007')}</th>
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
                      <span className="text-muted-foreground">{i18n.t('templates.k008')}</span>
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
                      title={i18n.t('templates.k009')}
                      aria-label={i18n.t('templates.k009')}
                      onClick={() => setTplDialog(t)}
                      className={ICON_BTN_CLS}
                    >
                      <Pencil className="h-4 w-4" />
                    </button>
                    <button
                      type="button"
                      title={i18n.t('templates.k010')}
                      aria-label={i18n.t('templates.k010')}
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
                  {i18n.t('templates.k047')}
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
        title={i18n.t('templates.k011')}
        description={
          pendingDelete
            ? i18n.t('templates.k012', { value: pendingDelete.name })
            : undefined
        }
        confirmLabel={i18n.t('templates.k010')}
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
              toast.success(i18n.t('templates.k013'));
            } catch (err) {
              setActionError(
                err instanceof ApiError ? err.message : i18n.t('templates.k014'),
              );
            }
          })();
        }}
      />
    </section>
  );
}

