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
                onClick={() => onImportScan(n)}
                disabled={acting !== null || n.paused}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Download className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? "推送（推空，停掉节点入站）" : "推送"}
                aria-label="推送"
                onClick={() => onPush(n)}
                disabled={acting !== null}
                className={`${ICON_BTN_CLS} disabled:opacity-50`}
              >
                <Upload className="h-4 w-4" />
              </button>
              <button
                type="button"
                title={n.paused ? "启用" : "暂停"}
                aria-label={n.paused ? "启用" : "暂停"}
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
                    onClick={() => onEdit(n)}
                  >
                    <Pencil className="mr-1.5 h-4 w-4" />
                    编辑
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onProbe(n)}
                  >
                    <RefreshCw className="mr-1.5 h-4 w-4" />
                    刷新状态
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onClearBlock(n)}
                  >
                    <Unlock className="mr-1.5 h-4 w-4" />
                    清除阻塞并重推
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-[13px]"
                    onClick={() => onDelete(n)}
                  >
                    <Trash2 className="mr-1.5 h-4 w-4" />
                    删除
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </CardFooter>
          </Card>

  );
}
