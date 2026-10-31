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
import { Segmented } from "./logs/Segmented";
import { CATEGORIES, LEVELS, RESULTS } from "./logs/constants";
import { LogRow } from "./logs/LogRow";

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
  // Buffered rows: fetched ahead (2 pages, ~200 rows) but not yet
  // rendered. Scrolling moves them into entries; the background then
  // refills the buffer, so scrolling never waits on the network.
  const [buffer, setBuffer] = useState<LogEntry[]>([]);
  const [bufferNextBefore, setBufferNextBefore] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  // atTop tracks whether the viewport sits at the newest end (top).
  // The 10s live tail only runs while atTop; scrolling into history
  // pauses it so a refresh never yanks the viewport back.
  const [atTop, setAtTop] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);
  const atTopRef = useRef(true);
  const fetchingRef = useRef(false);
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

  // Fetch `pages` pages ahead of `cursor` into the buffer (silent,
  // background). A superseded filter/tab discards the result via the
  // sequence guard, same as loadFresh.
  const prefetch = useCallback(
    async (cursor: string, pages: number) => {
      if (!cursor || pages <= 0) return;
      if (fetchingRef.current) return;
      fetchingRef.current = true;
      const seq = seqRef.current;
      let cur = cursor;
      const acc: LogEntry[] = [];
      try {
        for (let i = 0; i < pages && cur; i++) {
          const page = await api.logs({
            category,
            q: keyword || undefined,
            level: category === "core" ? level || undefined : undefined,
            result: category === "panel" ? result || undefined : undefined,
            before: cur,
          });
          if (seq !== seqRef.current) return;
          acc.push(...page.entries);
          cur = page.nextBefore;
        }
        if (seq !== seqRef.current) return;
        if (acc.length > 0) {
          setBuffer((prev) => [...prev, ...acc]);
          setBufferNextBefore(cur);
        }
      } catch {
        // Background prefetch failed: stay silent, the next scroll
        // trigger retries from the same cursor.
      } finally {
        fetchingRef.current = false;
      }
    },
    [category, keyword, level, result],
  );

  const loadFresh = useCallback(async () => {
    const seq = ++seqRef.current;
    setLoading(true);
    setLoadError(null);
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
      // Fresh list, fresh buffer: refill the 2-page (~200-row) buffer
      // behind the new first page so scrolling never waits.
      setBuffer([]);
      setBufferNextBefore("");
      void prefetch(page.nextBefore, 2);
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
  }, [category, keyword, level, result, prefetch]);

  // Fresh load on filter change and on top-bar refresh. A manual
  // refresh returns the viewport to the newest end first.
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
    void loadFresh();
  }, [loadFresh, refreshSignal]);

  // Live tail: refresh the latest page every 10s, but ONLY while the
  // viewport sits at the newest end. Scrolling into history pauses
  // it, so a refresh never yanks the viewport back to the top.
  // Latest-page reads are served from the server's in-memory tail,
  // so this poll stays cheap.
  useEffect(() => {
    if (!atTop) return;
    const id = window.setInterval(() => {
      void loadFresh();
    }, 10000);
    return () => window.clearInterval(id);
  }, [loadFresh, atTop]);

  // Returning to the top resumes live data immediately (not just on
  // the next 10s tick): the newest page loads at once.
  const prevAtTopRef = useRef(true);
  useEffect(() => {
    if (atTop && !prevAtTopRef.current) void loadFresh();
    prevAtTopRef.current = atTop;
  }, [atTop, loadFresh]);

  const applySearch = () => {
    setKeyword(draft.trim());
  };

  // Move the buffered rows into the rendered list and refill the
  // buffer behind them. Rendered rows only ever grow (append-only);
  // the buffer is what keeps scrolling wait-free.
  const consumeBuffer = useCallback(() => {
    if (buffer.length === 0) return;
    const cursor = bufferNextBefore;
    setEntries((prev) => [...prev, ...buffer]);
    setBuffer([]);
    setBufferNextBefore("");
    setNextBefore(cursor);
    if (cursor) void prefetch(cursor, 2);
  }, [buffer, bufferNextBefore, prefetch]);

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    const top = el.scrollTop <= 40;
    if (top !== atTopRef.current) {
      atTopRef.current = top;
      setAtTop(top);
    }
    if (el.scrollTop + el.clientHeight < el.scrollHeight - 300) return;
    if (buffer.length > 0) {
      consumeBuffer();
      return;
    }
    if (!nextBefore || fetchingRef.current) return;
    // Buffer empty (prefetch failed or not yet run): fetch the next
    // page directly into entries, then prefetch ahead.
    fetchingRef.current = true;
    const seq = seqRef.current;
    const cur = nextBefore;
    void (async () => {
      try {
        const page = await api.logs({
          category,
          q: keyword || undefined,
          level: category === "core" ? level || undefined : undefined,
          result: category === "panel" ? result || undefined : undefined,
          before: cur,
        });
        if (seq !== seqRef.current) return;
        setEntries((prev) => [...prev, ...page.entries]);
        setNextBefore(page.nextBefore);
        fetchingRef.current = false;
        if (page.nextBefore) void prefetch(page.nextBefore, 2);
      } catch {
        fetchingRef.current = false;
        // Silent: the next scroll trigger retries.
      }
    })();
  }, [
    buffer,
    nextBefore,
    category,
    keyword,
    level,
    result,
    consumeBuffer,
    prefetch,
  ]);

  // Rows: clock + level/result label + message. The level/result is
  // also shown as a left-border accent (error/failed) or row weight,
  // no badges, no per-row cards.

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
            setBuffer([]);
            setBufferNextBefore("");
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
          onClick={() => {
            containerRef.current?.scrollTo({ top: 0 });
            void loadFresh();
          }}
          disabled={loading}
        >
          <RefreshCw
            className={cn("h-4 w-4", loading && "animate-spin")}
          />
          刷新
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        核心与客户端连接来自 sing-box 运行日志；面板操作记录所有面板变更与结果，保留 90 天。
      </p>
      {/* One big container for the logs, old-panel style: monospace,
      dense lines, scrollable. Lines stay selectable: copying IPs and
      domains is the point. */}
      <div className="relative">
        <div className="rounded-[18px] border bg-card text-card-foreground shadow-sm">
          <div
            ref={containerRef}
            tabIndex={0}
            onScroll={handleScroll}
            onKeyDown={(e) => {
              // Focused log window + Enter: back to the newest end.
              if (e.key === "Enter") {
                containerRef.current?.scrollTo({ top: 0 });
                void loadFresh();
              }
            }}
            className="select-text max-h-[70vh] overflow-auto py-3 font-mono text-xs leading-5 outline-none"
          >
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
              <ul>{entries.map((e, i) => (
              <LogRow key={`${e.category}-${e.ts}-${i}`} e={e} formatTs={formatTs} />
            ))}</ul>
            )}
          </div>
        </div>
      </div>

    </div>
  );
}
