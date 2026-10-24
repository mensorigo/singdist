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
import { useEffect, useRef, useState, type ReactNode } from "react";

import {
  api,
  ApiError,
  type DashboardSummary,
  type TrendGranularity,
  type TrendPoint,
} from "@/lib/api";
import { formatBytes, formatRate, formatUptime } from "@/lib/format";
import { cn } from "@/lib/utils";

const POLL_MS = 2000;
const TREND_POLL_MS = 30000;

const GRANULARITIES: { key: TrendGranularity; label: string; range: string }[] = [
  { key: "minute", label: "分", range: "近 60 分钟" },
  { key: "hour", label: "时", range: "近 24 小时" },
  { key: "day", label: "日", range: "近 30 天" },
];

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
    timer.current = window.setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [refreshSignal]);

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
          label="独立 IP"
          value={String(data.distinctIPs)}
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

function DashCard({
  icon,
  label,
  value,
  sub,
  barPct,
  dot,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  sub?: string;
  barPct?: number;
  dot?: "on" | "off";
}) {
  return (
    <div className="flex h-full flex-col gap-0 rounded-xl border bg-card py-6 text-card-foreground shadow-sm">
      <div className="flex flex-col space-y-3 px-6">
        <div className="flex items-center gap-2">
          <span className="shrink-0 text-foreground/70">{icon}</span>
          <span className="text-sm text-muted-foreground">{label}</span>
          {dot && (
            <span
              className={cn(
                "ml-auto inline-block h-3 w-3 shrink-0 rounded-full",
                dot === "on" ? "bg-emerald-500" : "bg-red-500",
              )}
            />
          )}
        </div>
        <div className="truncate text-2xl font-semibold leading-8 tracking-tight tabular-nums">
          {value}
        </div>
        {sub && (
          <p className="line-clamp-2 text-xs leading-4 text-muted-foreground">
            {sub}
          </p>
        )}
        {barPct !== undefined && (
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-secondary">
            <div
              className="h-full rounded-full bg-primary transition-all duration-500"
              style={{ width: `${Math.max(0, Math.min(100, barPct))}%` }}
            />
          </div>
        )}
      </div>
    </div>
  );
}

function TrendCard({
  refreshSignal,
  timezone,
}: {
  refreshSignal: number;
  timezone: string;
}) {
  const [granularity, setGranularity] = useState<TrendGranularity>("day");
  const [points, setPoints] = useState<TrendPoint[]>([]);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const t = await api.dashboardTrend(granularity);
        if (!cancelled) setPoints(t.points);
      } catch {
        /* keep showing the previous points */
      }
    }
    void load();
    const id = window.setInterval(load, TREND_POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(id);
    };
  }, [granularity, refreshSignal]);

  const meta = GRANULARITIES.find((g) => g.key === granularity)!;
  const max = Math.max(...points.map((p) => p.up + p.down), 1);

  // Trend labels render in the panel timezone, not the browser's, so
  // day boundaries match the backend buckets.
  const labelFor = (ts: number) => {
    const d = new Date(ts * 1000);
    if (granularity === "day") {
      return new Intl.DateTimeFormat("en-CA", {
        timeZone: timezone,
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
      }).format(d);
    }
    return new Intl.DateTimeFormat("zh-CN", {
      timeZone: timezone,
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(d);
  };

  return (
    <div className="flex h-full flex-col rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <p className="text-base font-semibold">流量趋势</p>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {meta.range} · {timezone}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-block h-2 w-2 rounded-full bg-foreground" />
            上行
          </span>
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <span className="inline-block h-2 w-2 rounded-full bg-muted-foreground" />
            下行
          </span>
          <div className="flex rounded-lg bg-muted p-0.5">
            {GRANULARITIES.map((g) => (
              <button
                key={g.key}
                onClick={() => setGranularity(g.key)}
                className={cn(
                  "h-6 rounded-md px-2.5 text-xs transition-colors",
                  granularity === g.key
                    ? "bg-background font-medium text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {g.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-3 flex h-[170px] flex-1 items-end gap-[3px] border-b">
        {points.map((p) => {
          const total = p.up + p.down;
          const totalPct = Math.max((total / max) * 100, total > 0 ? 1.5 : 0);
          const upShare = total > 0 ? (p.up / total) * 100 : 0;
          return (
            <div
              key={p.ts}
              className="flex h-full min-w-0 flex-1 flex-col items-center justify-end"
              title={`${labelFor(p.ts)} 上行 ${formatBytes(p.up)} · 下行 ${formatBytes(
                p.down,
              )}`}
            >
              <div
                className="flex w-full max-w-6 flex-col justify-end overflow-hidden rounded-t-sm"
                style={{ height: `${totalPct}%` }}
              >
                <div
                  className="w-full shrink-0 bg-foreground"
                  style={{ height: `${upShare}%` }}
                />
                <div className="w-full flex-1 bg-muted-foreground/40" />
              </div>
            </div>
          );
        })}
        {points.length === 0 && (
          <div className="flex h-full w-full items-center justify-center text-[13px] font-medium text-muted-foreground">
            数据收集中…
          </div>
        )}
      </div>
      {points.length > 0 && (
        <div className="mt-2 flex justify-between text-xs text-muted-foreground">
          <span>{labelFor(points[0].ts)}</span>
          <span>{labelFor(points[points.length - 1].ts)}</span>
        </div>
      )}
    </div>
  );
}

function RealtimeCard({ data }: { data: DashboardSummary }) {
  return (
    <div className="flex h-full flex-col rounded-xl border bg-card px-6 py-6 text-card-foreground shadow-sm">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <p className="text-base font-semibold">实时网速</p>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-foreground" />
            上行 {formatRate(data.upRate)}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-2 rounded-full bg-muted-foreground" />
            下行 {formatRate(data.downRate)}
          </span>
        </div>
      </div>
      <div className="mt-3">
        <TrafficChart series={data.series} />
      </div>
    </div>
  );
}

/** Hand-rolled SVG line chart of the throughput series (no chart library). */
function TrafficChart({ series }: { series: DashboardSummary["series"] }) {
  const W = 720;
  const H = 170;
  const PAD = 6;

  if (series.length < 2) {
    return (
      <div className="flex h-[170px] items-center justify-center text-[13px] font-medium text-muted-foreground">
        数据收集中…
      </div>
    );
  }

  const max = Math.max(...series.map((s) => Math.max(s.up, s.down)), 1);
  const stepX = (W - PAD * 2) / (series.length - 1);
  const x = (i: number) => PAD + i * stepX;
  const y = (v: number) => H - PAD - (v / max) * (H - PAD * 2);

  const area = (key: "up" | "down") => {
    const pts = series.map((s, i) => `${x(i).toFixed(1)},${y(s[key]).toFixed(1)}`);
    return `M${x(0).toFixed(1)},${(H - PAD).toFixed(1)} L${pts.join(" L")} L${x(
      series.length - 1,
    ).toFixed(1)},${(H - PAD).toFixed(1)} Z`;
  };
  const line = (key: "up" | "down") =>
    `M${series.map((s, i) => `${x(i).toFixed(1)},${y(s[key]).toFixed(1)}`).join(" L")}`;

  return (
    <div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="h-auto w-full"
        role="img"
        aria-label="实时网速曲线"
        preserveAspectRatio="none"
      >
        {[1 / 3, 2 / 3].map((f) => {
          const gy = PAD + (H - PAD * 2) * f;
          return (
            <g key={f}>
              <line
                x1={PAD}
                x2={W - PAD}
                y1={gy}
                y2={gy}
                className="stroke-border"
                strokeWidth="1"
                strokeDasharray="4 4"
                vectorEffect="non-scaling-stroke"
              />
              <text
                x={W - PAD}
                y={gy - 5}
                textAnchor="end"
                fontSize="11"
                className="fill-muted-foreground"
              >
                {formatRate(max * (1 - f))}
              </text>
            </g>
          );
        })}
        <path d={area("down")} className="fill-muted-foreground/10" />
        <path
          d={line("down")}
          className="stroke-muted-foreground"
          fill="none"
          strokeWidth="1.5"
        />
        <path d={area("up")} className="fill-foreground/10" />
        <path d={line("up")} className="stroke-foreground" fill="none" strokeWidth="1.5" />
      </svg>
      <div className="mt-2 flex justify-between text-xs text-muted-foreground">
        <span>
          {new Date(series[0].ts * 1000).toLocaleTimeString("zh-CN", {
            hour12: false,
          })}
        </span>
        <span>现在</span>
      </div>
    </div>
  );
}
