import { Download, Ellipsis, Pause, Pencil, Play, RefreshCw, Trash2, Unlock, Upload } from "lucide-react";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { NodeView } from "@/lib/api";
import { ICON_BTN_CLS, fmtBytes, fmtMem, fmtTime } from "./format";
import i18n from "@/lib/i18n";

export function NodeCard({
  n,
  acting,
  onImportScan,
  onPush,
  onTogglePause,
  onEdit,
  onProbe,
  onClearBlock,
  onDelete,
}: {
  n: NodeView;
  acting: string | null;
  onImportScan: (n: NodeView) => void;
  onPush: (n: NodeView) => void;
  onTogglePause: (n: NodeView) => void;
  onEdit: (n: NodeView) => void;
  onProbe: (n: NodeView) => void;
  onClearBlock: (n: NodeView) => void;
  onDelete: (n: NodeView) => void;
}) {
  return (
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
                      {i18n.t('nodes.k075')}
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
                    {n.paused ? i18n.t('nodes.k026') : n.online ? i18n.t('nodes.k027') : i18n.t('nodes.k028')}
                  </span>
                </span>
              </div>
              <p className="text-xs text-muted-foreground">
                {n.lastVersion ? `agent ${n.lastVersion} · ` : ""}
                {i18n.t('nodes.k076', { value: n.lastInboundCount })}
              </p>
            </CardHeader>
            <CardContent className="flex-1 space-y-3 text-sm">
              <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
                <dt className="text-muted-foreground">{i18n.t('nodes.k029')}</dt>
                <dd className="min-w-0 truncate text-foreground">
                  {n.address}
                </dd>
                {n.publicHost && (
                  <>
                    <dt className="text-muted-foreground">{i18n.t('nodes.k030')}</dt>
                    <dd className="min-w-0 truncate text-foreground">
                      {n.publicHost}
                    </dd>
                  </>
                )}
              </dl>
              <div className="grid grid-cols-2 gap-2">
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">{i18n.t('nodes.k031')}</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available
                      ? `${n.agentSystem.cpuPct.toFixed(2)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {i18n.t('nodes.k077')}
                    {n.agentSystem?.available
                      ? n.agentSystem.load1.toFixed(2)
                      : "—"}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">{i18n.t('nodes.k032')}</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available
                      ? `${n.agentSystem.memPct.toFixed(0)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentSystem?.available
                      ? `${fmtMem(n.agentSystem.memUsed)} / ${fmtMem(n.agentSystem.memTotal)}`
                      : i18n.t('nodes.k033')}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">{i18n.t('nodes.k034')}</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentSystem?.available && n.agentSystem.diskTotal > 0
                      ? `${n.agentSystem.diskPct.toFixed(2)}%`
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentSystem?.available && n.agentSystem.diskTotal > 0
                      ? `${fmtMem(n.agentSystem.diskUsed)} / ${fmtMem(n.agentSystem.diskTotal)}`
                      : i18n.t('nodes.k033')}
                  </p>
                </div>
                <div className="rounded-xl bg-muted/50 px-3 py-2.5 shadow-sm">
                  <p className="text-xs text-muted-foreground">{i18n.t('nodes.k035')}</p>
                  <p className="mt-0.5 text-lg font-semibold leading-none">
                    {n.agentTraffic
                      ? fmtBytes(n.agentTraffic.up + n.agentTraffic.down)
                      : "—"}
                  </p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {n.agentTraffic
                      ? i18n.t('nodes.k036', { up: fmtBytes(n.agentTraffic.up), down: fmtBytes(n.agentTraffic.down) })
                      : i18n.t('nodes.k033')}
                  </p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('nodes.k078')}
                {n.lastPushResult
                  ? `${n.lastPushResult}（${fmtTime(n.lastPushAt)}）`
                  : "—"}
              </p>
              {n.agentStatusAt === 0 && (
                <p className="text-[13px] text-muted-foreground">
                  {i18n.t('nodes.k079')}
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
                  {i18n.t('nodes.k080')}
                </p>
              )}
            </CardContent>
            <CardFooter className="mt-auto flex flex-wrap justify-end gap-1">
              <button
                type="button"
                title={i18n.t('nodes.k037')}
                aria-label={i18n.t('nodes.k037')}
                onClick={() => onImportScan(n)}
                disabled={acting !== null || n.paused}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Download className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? i18n.t('nodes.k038') : i18n.t('nodes.k039')}
                aria-label={i18n.t('nodes.k039')}
                onClick={() => onPush(n)}
                disabled={acting !== null}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Upload className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? i18n.t('nodes.k040') : i18n.t('nodes.k041')}
                aria-label={n.paused ? i18n.t('nodes.k040') : i18n.t('nodes.k041')}
                onClick={() => onTogglePause(n)}
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
                    title={i18n.t('nodes.k042')}
                    aria-label={i18n.t('nodes.k042')}
                    disabled={acting !== null}
                    className={`${ICON_BTN_CLS} disabled:opacity-50`}
                  >
                    <Ellipsis className="h-4 w-4" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-32">
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onEdit(n)}
                  >
                    <Pencil className="mr-1.5 h-4 w-4" />
                    {i18n.t('nodes.k081')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onProbe(n)}
                  >
                    <RefreshCw className="mr-1.5 h-4 w-4" />
                    {i18n.t('nodes.k082')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onClearBlock(n)}
                  >
                    <Unlock className="mr-1.5 h-4 w-4" />
                    {i18n.t('nodes.k083')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onDelete(n)}
                  >
                    <Trash2 className="mr-1.5 h-4 w-4" />
                    {i18n.t('nodes.k084')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </CardFooter>
          </Card>

  );
}
