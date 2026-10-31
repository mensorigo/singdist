import type { DashboardSummary } from "@/lib/api";
import { formatRate } from "@/lib/format";

export function TrafficChart({ series }: { series: DashboardSummary["series"] }) {
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
