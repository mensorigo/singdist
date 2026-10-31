import {
  Activity,
  Cpu,
  Database,
  HardDrive,
  MemoryStick,
  Server,
  TrendingUp,
  TriangleAlert,
  Users,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";

import {
  api,
  ApiError,
  type DashboardSummary,
} from "@/lib/api";
import { formatBytes, formatUptime } from "@/lib/format";
import { DashCard } from "./dashboard/DashCard";
import { TrendCard } from "./dashboard/TrendCard";
import { RealtimeCard } from "./dashboard/RealtimeCard";

function formatCoreVersion(v: string): string {
  const token = v.replace(/sing-box/i, "").trim().split(/\s+/)[0] ?? "";
  if (!token) return "—";
  return /^\d/.test(token) ? `V${token}` : token;
}

export default function DashboardPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [data, setData] = useState<DashboardSummary | null>(null);
  const [fetchError, setFetchError] = useState(false);
  const timer = useRef<number | null>(null);
  // Summary poll interval from settings (seconds); default 3 matches
  // the backend default until settings load.
  const [pollMs, setPollMs] = useState(3000);
  const [online, setOnline] = useState<{
    total: number;
  } | null>(null);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const v = await api.settings.get();
        if (!cancelled) setPollMs(v.dashIntervalSecs * 1000);
      } catch {
        /* keep the default interval */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [refreshSignal]);

  // Runs on mount, on every poll tick, and whenever the top-bar refresh
  // button bumps refreshSignal — an in-place refetch, never a remount.
  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const s = await api.dashboardSummary();
        if (!cancelled) {
          setData(s);
          setFetchError(false);
        }
      } catch (err) {
        if (!cancelled) {
          if (err instanceof ApiError && err.status === 401) {
            window.location.reload();
            return;
          }
          setFetchError(true);
        }
      }
    }
    void load();
    timer.current = window.setInterval(load, pollMs);
    return () => {
      cancelled = true;
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [refreshSignal, pollMs]);

  // Online-device counts ride the dashboard poll timer; failures
  // stay silent, the card keeps its last value.
  useEffect(() => {
    let cancelled = false;
    let timer: number | null = null;
    const load = async () => {
      try {
        const r = await api.subscriptions.onlineIps();
        if (!cancelled)
          setOnline({ total: r.total });
      } catch {
        /* silent: stats only */
      }
    };
    void load();
    timer = window.setInterval(load, pollMs);
    return () => {
      cancelled = true;
      if (timer) window.clearInterval(timer);
    };
  }, [refreshSignal, pollMs]);

  if (!data) {
    return (
      <p className="text-sm text-muted-foreground">
        {fetchError ? "暂时无法获取数据，正在重试…" : "数据加载中…"}
      </p>
    );
  }

  const sys = data.system;
  const memPct =
    sys.memTotalBytes > 0 ? (sys.memUsedBytes / sys.memTotalBytes) * 100 : 0;
  const diskPct =
    sys.diskTotalBytes > 0 ? (sys.diskUsedBytes / sys.diskTotalBytes) * 100 : 0;

  return (
    <div className="space-y-4">
      {!data.coreOnline && (
        <div
          role="alert"
          className="flex items-start gap-3 rounded-md border border-destructive/50 bg-destructive/10 px-3.5 py-2.5 text-[13px] text-destructive"
        >
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <div>
            <p className="font-medium">sing-box 核心当前不可达</p>
            <p className="text-destructive/80">
              流量与连接数据已暂停更新。请检查 sing-box 服务是否正在运行，以及 Clash API 是否可访问。
            </p>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <DashCard
          icon={<Activity className="h-5 w-5" />}
          label="sing-box"
          value={formatCoreVersion(data.coreVersion)}
          dot={data.coreOnline ? "on" : "off"}
          sub={
            data.coreOnline
              ? data.coreUptimeSecs > 0
                ? `运行中 · 已运行 ${formatUptime(data.coreUptimeSecs)}`
                : "运行中"
              : "核心离线"
          }
        />
        <DashCard
          icon={<Server className="h-5 w-5" />}
          label="主机"
          value={sys.hostname || "—"}
          sub={`${sys.os ? `${sys.os} · ` : ""}已运行 ${formatUptime(
            sys.hostUptimeSecs,
          )}`}
        />
        <DashCard
          icon={<Cpu className="h-5 w-5" />}
          label="CPU"
          value={`${sys.cpuPercent.toFixed(1)}%`}
          sub={`负载 ${sys.loadAvg.map((v) => v.toFixed(2)).join(" / ")}`}
          barPct={sys.cpuPercent}
        />
        <DashCard
          icon={<MemoryStick className="h-5 w-5" />}
          label="内存"
          value={formatBytes(sys.memUsedBytes)}
          sub={`共 ${formatBytes(sys.memTotalBytes)}`}
          barPct={memPct}
        />
        <DashCard
          icon={<HardDrive className="h-5 w-5" />}
          label="磁盘"
          value={formatBytes(sys.diskUsedBytes)}
          sub={`共 ${formatBytes(sys.diskTotalBytes)}`}
          barPct={diskPct}
        />
        <DashCard
          icon={<Database className="h-5 w-5" />}
          label="总已用流量"
          value={formatBytes(data.traffic.totalUp + data.traffic.totalDown)}
          sub="全订阅累计"
        />
        <DashCard
          icon={<TrendingUp className="h-5 w-5" />}
          label="今日流量"
          value={formatBytes(data.traffic.todayUp + data.traffic.todayDown)}
          sub={`按 ${data.timezone} 日期`}
        />
        <DashCard
          icon={<Users className="h-5 w-5" />}
          label="在线设备"
          value={online ? String(online.total) : "—"}
          sub={`共 ${data.activeConns} 条连接`}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <TrendCard refreshSignal={refreshSignal} timezone={data.timezone} />
        <RealtimeCard data={data} />
      </div>
    </div>
  );
}

