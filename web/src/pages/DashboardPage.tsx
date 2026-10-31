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
import i18n from "@/lib/i18n";

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
        {fetchError ? i18n.t('dashboard.k001') : i18n.t('dashboard.k002')}
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
            <p className="font-medium">{i18n.t('dashboard.k003')}</p>
            <p className="text-destructive/80">
              {i18n.t('dashboard.k029')}
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
                ? i18n.t('dashboard.k004', { value: formatUptime(data.coreUptimeSecs) })
                : i18n.t('dashboard.k005')
              : i18n.t('dashboard.k006')
          }
        />
        <DashCard
          icon={<Server className="h-5 w-5" />}
          label={i18n.t('dashboard.k007')}
          value={sys.hostname || "—"}
          sub={i18n.t("dashboard.running", { os: sys.os ? `${sys.os} · ` : "", uptime: formatUptime(sys.hostUptimeSecs) })}
        />
        <DashCard
          icon={<Cpu className="h-5 w-5" />}
          label="CPU"
          value={`${sys.cpuPercent.toFixed(1)}%`}
          sub={i18n.t('dashboard.k008', { value: sys.loadAvg.map((v) => v.toFixed(2)).join(" / ") })}
          barPct={sys.cpuPercent}
        />
        <DashCard
          icon={<MemoryStick className="h-5 w-5" />}
          label={i18n.t('dashboard.k009')}
          value={formatBytes(sys.memUsedBytes)}
          sub={i18n.t('dashboard.k010', { value: formatBytes(sys.memTotalBytes) })}
          barPct={memPct}
        />
        <DashCard
          icon={<HardDrive className="h-5 w-5" />}
          label={i18n.t('dashboard.k011')}
          value={formatBytes(sys.diskUsedBytes)}
          sub={i18n.t('dashboard.k012', { value: formatBytes(sys.diskTotalBytes) })}
          barPct={diskPct}
        />
        <DashCard
          icon={<Database className="h-5 w-5" />}
          label={i18n.t('dashboard.k013')}
          value={formatBytes(data.traffic.totalUp + data.traffic.totalDown)}
          sub={i18n.t('dashboard.k014')}
        />
        <DashCard
          icon={<TrendingUp className="h-5 w-5" />}
          label={i18n.t('dashboard.k015')}
          value={formatBytes(data.traffic.todayUp + data.traffic.todayDown)}
          sub={i18n.t('dashboard.k016', { value: data.timezone })}
        />
        <DashCard
          icon={<Users className="h-5 w-5" />}
          label={i18n.t('dashboard.k017')}
          value={online ? String(online.total) : "—"}
          sub={i18n.t('dashboard.k018', { value: data.activeConns })}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <TrendCard refreshSignal={refreshSignal} timezone={data.timezone} />
        <RealtimeCard data={data} />
      </div>
    </div>
  );
}

