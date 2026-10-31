import { request } from "./client";

export type LogCategory = "core" | "panel" | "connection" | "config";

export interface LogEntry {
  ts: number;
  category: LogCategory;
  level?: string;
  message?: string;
  source?: string;
  destination?: string;
  tag?: string;
  user?: string;
  action?: string;
  target?: string;
  result?: string;
  detail?: string;
  ip?: string;
  applied_hash?: string;
  summary?: string;
}

export interface LogPage {
  entries: LogEntry[];
  nextBefore: string;
}

export interface LogQuery {
  category: LogCategory;
  q?: string;
  level?: string;
  result?: string;
  before?: string;
  limit?: number;
}


export const logsApi = (params: LogQuery) => {
  const qs = new URLSearchParams();
  qs.set("category", params.category);
  if (params.q) qs.set("q", params.q);
  if (params.level) qs.set("level", params.level);
  if (params.result) qs.set("result", params.result);
  if (params.before) qs.set("before", params.before);
  qs.set("limit", String(params.limit ?? 100));
  return request<LogPage>(`/api/logs?${qs.toString()}`);
};
