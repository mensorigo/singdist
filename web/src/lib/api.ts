export interface AuthStatus {
  authenticated: boolean;
  username?: string;
}

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
  series: DashboardSample[];
  system: SystemInfo;
  traffic: TrafficInfo;
  panelUptimeSecs: number;
}

export interface CertInfo {
  id: string;
  note: string;
  source: string;
  importedAt: number;
  active: boolean;
  commonName: string;
  dnsNames: string[];
  issuer: string;
  serial: string;
  notBefore: number;
  notAfter: number;
  daysLeft: number;
  fingerprint: string;
  keyType: string;
  keyMatches: boolean;
  selfSigned: boolean;
  expired: boolean;
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

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  const data = (await resp.json().catch(() => ({}))) as Record<string, unknown>;
  if (!resp.ok) {
    throw new ApiError(
      resp.status,
      typeof data.code === "string" ? data.code : "request_failed",
      typeof data.message === "string" ? data.message : resp.statusText,
    );
  }
  return data as T;
}

export interface CertList {
  active: CertInfo | null;
  versions: CertInfo[];
}

export const api = {
  status: () => request<AuthStatus>("/api/auth/status"),
  login: (username: string, password: string) =>
    request<{ username: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  dashboardSummary: () =>
    request<DashboardSummary>("/api/dashboard/summary"),
  dashboardTrend: (granularity: TrendGranularity) =>
    request<DashboardTrend>(`/api/dashboard/trend?granularity=${granularity}`),
  certs: {
    list: () => request<CertList>("/api/certs"),
    upload: (certPem: string, keyPem: string, note: string, activate: boolean) =>
      request<CertInfo>("/api/certs", {
        method: "POST",
        body: JSON.stringify({ certPem, keyPem, note, activate }),
      }),
    selfSigned: (
      names: string[],
      days: number,
      note: string,
      activate: boolean,
    ) =>
      request<CertInfo>("/api/certs/selfsigned", {
        method: "POST",
        body: JSON.stringify({ names, days, note, activate }),
      }),
    activate: (id: string) =>
      request<CertInfo>(`/api/certs/${encodeURIComponent(id)}/activate`, {
        method: "POST",
      }),
    remove: (id: string) =>
      request<{ ok: boolean }>(`/api/certs/${encodeURIComponent(id)}`, {
        method: "DELETE",
      }),
  },
};
