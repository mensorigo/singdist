import { useCallback, useEffect, useRef, useState } from "react";
import { RefreshCw, Search } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  api,
  ApiError,
  type LogCategory,
  type LogEntry,
} from "@/lib/api";
import { cn } from "@/lib/utils";

// Segmented mirrors the panel's other segmented controls.
function Segmented<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: { value: T; label: string }[];
  onChange: (v: T) => void;
}) {
  return (
    <div className="inline-flex rounded-xl bg-muted p-1">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cn(
            "h-7 rounded-lg px-3 text-[13px] transition-colors",
            value === o.value
              ? "bg-background font-medium shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

const CATEGORIES: { value: LogCategory; label: string }[] = [
  { value: "core", label: "核心" },
  { value: "panel", label: "面板操作" },
  { value: "connection", label: "客户端连接" },
];

const LEVELS = [
  { value: "", label: "全部级别" },
  { value: "INFO", label: "INFO" },
  { value: "WARN", label: "WARN" },
  { value: "ERROR", label: "ERROR" },
];

const RESULTS = [
  { value: "", label: "全部结果" },
  { value: "success", label: "成功" },
  { value: "failed", label: "失败" },
];

export default function LogsPage({
  refreshSignal,
}: {
  refreshSignal: number;
}) {
  const [category, setCategory] = useState<LogCategory>("core");
  const [draft, setDraft] = useState("");
  const [keyword, setKeyword] = useState("");
  const [level, setLevel] = useState("");
  const [result, setResult] = useState("");
  const [timezone, setTimezone] = useState("Asia/Shanghai");

  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [nextBefore, setNextBefore] = useState("");
  const [paged, setPaged] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  // Sequence guard: a slow response from a superseded filter/tab must
  // never overwrite newer data. loadedRef tracks whether any page has
  // ever rendered, so a failed background refresh keeps stale rows
  // instead of blanking the list.
  const seqRef = useRef(0);
  const loadedRef = useRef(false);

  // Panel timezone for rendering timestamps (default stands on failure).
  useEffect(() => {
    let cancelled = false;
    api.settings
      .get()
      .then((v) => {
        if (!cancelled && v.timezone) setTimezone(v.timezone);
      })
      .catch(() => {
        /* default stands */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const formatTs = useCallback(
    (tsMs: number) => {
      try {
        return new Intl.DateTimeFormat("zh-CN", {
          timeZone: timezone,
          year: "numeric",
          month: "2-digit",
          day: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
          hour12: false,
        }).format(new Date(tsMs));
      } catch {
        return new Date(tsMs).toLocaleString("zh-CN", { hour12: false });
      }
    },
    [timezone],
  );

  const loadFresh = useCallback(async () => {
    const seq = ++seqRef.current;
    setLoading(true);
    setLoadError(null);
    setPaged(false);
    try {
      const page = await api.logs({
        category,
        q: keyword || undefined,
        level: category === "core" ? level || undefined : undefined,
        result: category === "panel" ? result || undefined : undefined,
      });
      if (seq !== seqRef.current) return;
      setEntries(page.entries);
      setNextBefore(page.nextBefore);
      loadedRef.current = true;
    } catch (err) {
      if (seq !== seqRef.current) return;
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      if (loadedRef.current) {
        // Background refresh failed: keep the stale rows, say so.
        toast.error("日志刷新失败，显示的仍是上次的数据");
      } else {
        setEntries([]);
        setNextBefore("");
        setLoadError(
          err instanceof ApiError && err.code === "log_unavailable"
            ? "核心日志暂时无法读取，请稍后重试"
            : "日志加载失败，请稍后重试",
        );
      }
    } finally {
      if (seq === seqRef.current) setLoading(false);
    }
  }, [category, keyword, level, result]);

  // Fresh load on filter change and on top-bar refresh.
  useEffect(() => {
    void loadFresh();
  }, [loadFresh, refreshSignal]);

  // Live tail: refresh the latest page every 10s unless the user has
  // paged into older history. Latest-page reads are served from the
  // server's in-memory tail, so this poll stays cheap.
  useEffect(() => {
    if (paged) return;
    const id = window.setInterval(() => {
      void loadFresh();
    }, 10000);
    return () => window.clearInterval(id);
  }, [loadFresh, paged]);

  const applySearch = () => {
    setKeyword(draft.trim());
  };

  const loadMore = async () => {
    if (!nextBefore || loadingMore) return;
    const seq = seqRef.current;
    setLoadingMore(true);
    try {
      const page = await api.logs({
        category,
        q: keyword || undefined,
        level: category === "core" ? level || undefined : undefined,
        result: category === "panel" ? result || undefined : undefined,
        before: nextBefore,
      });
      if (seq !== seqRef.current) return;
      setEntries((prev) => [...prev, ...page.entries]);
      setNextBefore(page.nextBefore);
      setPaged(true);
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "加载更多失败，请重试",
      );
    } finally {
      setLoadingMore(false);
    }
  };

  // Rows: clock + level/result label + message. The level/result is
  // also shown as a left-border accent (error/failed) or row weight,
  // no badges, no per-row cards.
  const renderRow = (e: LogEntry, i: number) => {
    const key = `${e.category}-${e.ts}-${i}`;
    const time = (
      <span className="shrink-0 font-normal tabular-nums text-muted-foreground">
        {formatTs(e.ts)}
      </span>
    );
    if (e.category === "panel") {
      const failed = e.result === "failed";
      return (
        <li
          key={key}
          className={cn(
            "flex gap-2 border-l-2 border-transparent px-4 py-px",
            failed && "border-foreground font-semibold",
          )}
        >
          {time}
          <span
            className={cn(
              "w-8 shrink-0 tabular-nums",
              failed ? "text-foreground" : "text-muted-foreground",
            )}
          >
            {failed ? "失败" : "成功"}
          </span>
          <span className="min-w-0 flex-1 font-normal whitespace-pre-wrap break-all">
            {e.action}
            {e.target ? ` ${e.target}` : ""}
            {failed && e.detail ? `（${e.detail}）` : ""}
          </span>
        </li>
      );
    }
    if (e.category === "connection") {
      // One row is one connection: origin IP -> destination, plus the
      // subscription and node it used. A part stays blank when that
      // line fell outside the journal window.
      const main = [e.source, e.destination]
        .filter(Boolean)
        .join(" → ");
      const sub = [
        e.user ? `订阅 ${e.user}` : "",
        e.tag ? `节点 ${e.tag}` : "",
      ]
        .filter(Boolean)
        .join(" · ");
      return (
        <li
          key={key}
          className="flex gap-2 border-l-2 border-transparent px-4 py-px"
        >
          {time}
          <span className="min-w-0 flex-1 font-normal whitespace-pre-wrap break-all">
            {main || e.message || "—"}
            {sub && (
              <span className="text-muted-foreground"> {sub}</span>
            )}
          </span>
        </li>
      );
    }
    const accent =
      e.level === "ERROR" || e.level === "FATAL"
        ? "border-foreground font-semibold"
        : e.level === "WARN"
          ? "font-medium"
          : "";
    return (
      <li
        key={key}
        className={cn(
          "flex gap-2 border-l-2 border-transparent px-4 py-px",
          accent,
        )}
      >
        {time}
        <span
          className={cn(
            "w-12 shrink-0 tabular-nums",
            e.level === "ERROR" || e.level === "FATAL"
              ? "text-foreground"
              : "text-muted-foreground",
          )}
        >
          {e.level}
        </span>
        <span className="min-w-0 flex-1 font-normal whitespace-pre-wrap break-all">
          {e.message}
        </span>
      </li>
    );
  };

  return (
    <div className="space-y-4">
      {/* Filter row, uncarded like the old page; the logs below live in
      one big container. */}
      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          value={category}
          options={CATEGORIES}
          onChange={(v) => {
            // Switching category clears the list so stale rows from
            // the old category never flash under the new tab; the
            // placeholder shows until the new page arrives.
            setEntries([]);
            setNextBefore("");
            loadedRef.current = false;
            setCategory(v);
            setDraft("");
            setKeyword("");
            setLevel("");
            setResult("");
          }}
        />
        <div className="relative min-w-52 flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") applySearch();
            }}
            placeholder={
              category === "panel"
                ? "搜索操作、对象…"
                : category === "connection"
                  ? "搜索 IP、域名、订阅…"
                  : "搜索日志内容…"
            }
            className="pl-9"
          />
        </div>
        <Button type="button" onClick={applySearch}>
          搜索
        </Button>
        {category === "core" && (
          <Segmented value={level} options={LEVELS} onChange={setLevel} />
        )}
        {category === "panel" && (
          <Segmented value={result} options={RESULTS} onChange={setResult} />
        )}
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => void loadFresh()}
          disabled={loading}
        >
          <RefreshCw
            className={cn("h-4 w-4", loading && "animate-spin")}
          />
          刷新
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        核心与客户端连接来自 sing-box 运行日志；面板操作记录所有面板变更与结果，保留
        90 天。
      </p>
      {/* One big container for the logs, old-panel style: monospace,
      dense lines, scrollable. Lines stay selectable: copying IPs and
      domains is the point. */}
      <div className="relative">
        <div className="rounded-xl border bg-card text-card-foreground shadow-sm">
          <div className="select-text max-h-[70vh] overflow-auto py-3 font-mono text-xs leading-5">
            {entries.length === 0 ? (
              loading ? (
                <p className="px-4 py-8 text-center font-sans text-muted-foreground">
                  日志加载中…
                </p>
              ) : loadError ? (
                <p className="px-4 py-8 text-center font-sans text-destructive">
                  {loadError}
                </p>
              ) : (
                <p className="px-4 py-8 text-center font-sans text-muted-foreground">
                  {keyword || level || result ? "没有匹配的日志" : "暂无日志"}
                </p>
              )
            ) : (
              // Refreshes swap rows in place; the list never blanks.
              <ul>{entries.map(renderRow)}</ul>
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span>共 {entries.length} 行</span>
        {nextBefore ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void loadMore()}
            disabled={loadingMore}
          >
            {loadingMore ? "加载中…" : "加载更多"}
          </Button>
        ) : (
          entries.length > 0 && <span>没有更多日志</span>
        )}
      </div>
    </div>
  );
}
