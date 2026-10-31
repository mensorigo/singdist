import { request } from "./client";
import type { LocalMachine } from "./nodes";

export interface DashboardSample {
  ts: number;
  up: number;
  down: number;
}

export interface SystemInfo {
  hostname: string;
  os: string;
  cpuPercent: number;
  loadAvg: [number, number, number];
  memUsedBytes: number;
  memTotalBytes: number;
  diskUsedBytes: number;
  diskTotalBytes: number;
  hostUptimeSecs: number;
  netRxBytes: number;
  netTxBytes: number;
}

export interface TrafficInfo {
  todayUp: number;
  todayDown: number;
  totalUp: number;
  totalDown: number;
}

export interface DashboardSummary {
  coreOnline: boolean;
  coreVersion: string;
  coreUptimeSecs: number;
  upRate: number;
  downRate: number;
  uploadTotal: number;
  downloadTotal: number;
  activeConns: number;
  distinctIPs: number;
  series: DashboardSample[];
  system: SystemInfo;
  traffic: TrafficInfo;
  panelUptimeSecs: number;
  timezone: string;
}

export type TrendGranularity = "minute" | "hour" | "day";

export interface TrendPoint {
  ts: number;
  up: number;
  down: number;
}

export interface DashboardTrend {
  granularity: TrendGranularity;
  points: TrendPoint[];
}


export const dashboardSummary = () =>
  request<DashboardSummary>("/api/dashboard/summary");
export const dashboardTrend = (granularity: TrendGranularity) =>
  request<DashboardTrend>(`/api/dashboard/trend?granularity=${granularity}`);
export const localMachine = () => request<LocalMachine>("/api/local-machine");
