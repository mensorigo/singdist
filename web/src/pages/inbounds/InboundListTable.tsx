import { Pencil, Trash } from "lucide-react";
import { EnabledSwitch } from "@/components/enabled-switch";
import { Button } from "@/components/ui/button";
import type { InboundView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { ICON_BTN_CLS, PROTOCOL_LABEL, PROTOCOL_TRANSPORT } from "./constants";
import { DiffsList } from "./DiffsList";
import { StatusBadge } from "./StatusBadge";
import type { MachineInfo } from "./constants";
import i18n from "@/lib/i18n";

export function InboundListTable({
  items,
  machines,
  showWarnings,
  busyId,
  actionError,
  onCreate,
  onToggleEnabled,
  onEdit,
  onDelete,
}: {
  items: InboundView[];
  machines: Record<number, MachineInfo>;
  showWarnings: boolean;
  busyId: number | null;
  actionError: string | null;
  onCreate: () => void;
  onToggleEnabled: (v: InboundView) => void;
  onEdit: (v: InboundView) => void;
  onDelete: (v: InboundView) => void;
}) {
  return (
      <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <div className="flex items-center justify-between">
          <p className="text-base font-semibold">{i18n.t('inbounds.k037')}</p>
          <Button
            onClick={onCreate}
            className="h-8 rounded-xl px-3"
          >
            {i18n.t('inbounds.k069')}
          </Button>
        </div>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[920px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k038')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k039')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k040')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k041')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k042')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k043')}</th>
                <th className="py-2 pr-3 font-medium">{i18n.t('inbounds.k044')}</th>
                <th className="py-2 font-medium">{i18n.t('inbounds.k045')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((v) => (
                <tr key={v.id} className="border-b last:border-0">
                  <td
                    className={cn(
                      "py-2.5 pr-3 font-medium",
                      !v.enabled && "text-muted-foreground",
                    )}
                  >
                    {v.name}
                  </td>
                  <td className="py-2.5 pr-3">
                    {machines[v.id]?.name ?? (v.runLocal ? i18n.t('inbounds.k046') : "—")}
                  </td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {machines[v.id]?.ip ?? "—"}
                  </td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {machines[v.id]?.port ?? v.port}
                  </td>
                  <td className="py-2.5 pr-3">
                    <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {PROTOCOL_LABEL[v.protocol]}
                    </span>
                  </td>
                  <td className="py-2.5 pr-3">
                    <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {PROTOCOL_TRANSPORT[v.protocol]}
                    </span>
                  </td>
                  <td className="py-2.5 pr-3">
                    <StatusBadge
                      status={v.status}
                      showWarnings={showWarnings}
                    />
                    {showWarnings &&
                      v.status === "drifted" &&
                      v.diffs &&
                      v.diffs.length > 0 && <DiffsList diffs={v.diffs} />}
                  </td>
                  <td className="py-2.5">
                    <div className="flex items-center gap-3">
                      <EnabledSwitch
                        enabled={v.enabled}
                        disabled={busyId === v.id}
                        onToggle={() => onToggleEnabled(v)}
                      />
                      <button
                        type="button"
                        title={i18n.t('inbounds.k047')}
                        aria-label={i18n.t('inbounds.k047')}
                        disabled={busyId === v.id}
                        onClick={() => onEdit(v)}
                        className={`${ICON_BTN_CLS} disabled:opacity-50`}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        title={i18n.t('inbounds.k048')}
                        aria-label={i18n.t('inbounds.k048')}
                        disabled={busyId === v.id}
                        onClick={() => onDelete(v)}
                        className={`${ICON_BTN_CLS} disabled:opacity-50`}
                      >
                        <Trash className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {items.length === 0 && (
                <tr>
                  <td
                    colSpan={8}
                    className="py-6 text-center text-sm text-muted-foreground"
                  >
                    {i18n.t('inbounds.k070')}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
  );
}
