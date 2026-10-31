import { Pencil, Trash } from "lucide-react";
import { EnabledSwitch } from "@/components/enabled-switch";
import { Button } from "@/components/ui/button";
import type { InboundView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { ICON_BTN_CLS, PROTOCOL_LABEL, PROTOCOL_TRANSPORT } from "./constants";
import { DiffsList } from "./DiffsList";
import { StatusBadge } from "./StatusBadge";
import type { MachineInfo } from "./constants";

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
          <p className="text-base font-semibold">入站列表</p>
          <Button
            onClick={onCreate}
            className="h-8 rounded-xl px-3"
          >
            新建入站
          </Button>
        </div>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[920px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">名称</th>
                <th className="py-2 pr-3 font-medium">运行机器</th>
                <th className="py-2 pr-3 font-medium">机器IP</th>
                <th className="py-2 pr-3 font-medium">端口</th>
                <th className="py-2 pr-3 font-medium">协议</th>
                <th className="py-2 pr-3 font-medium">传输</th>
                <th className="py-2 pr-3 font-medium">状态</th>
                <th className="py-2 font-medium">操作</th>
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
                    {machines[v.id]?.name ?? (v.runLocal ? "本机" : "—")}
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
                        title="编辑"
                        aria-label="编辑"
                        disabled={busyId === v.id}
                        onClick={() => onEdit(v)}
                        className={`${ICON_BTN_CLS} disabled:opacity-50`}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        title="删除"
                        aria-label="删除"
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
                    暂无入站，点右上角「新建入站」创建
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
  );
}
