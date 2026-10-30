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
  distinctIPs: number;
  series: DashboardSample[];
  system: SystemInfo;
  traffic: TrafficInfo;
  panelUptimeSecs: number;
  timezone: string;
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

// Config-change notifications: any successful mutation on a
// config-affecting endpoint tells subscribers (the top-bar sync
// control) to reload the sync status immediately, instead of waiting
// for a manual refresh. Reads and auth calls never emit.
type ConfigChangeListener = () => void;

const configChangeListeners = new Set<ConfigChangeListener>();

export function onConfigChanged(fn: ConfigChangeListener): () => void {
  configChangeListeners.add(fn);
  return () => {
    configChangeListeners.delete(fn);
  };
}

const CONFIG_PATHS = [
  "/api/inbounds",
  "/api/subscriptions",
  "/api/templates",
  "/api/certs",
  "/api/settings",
  "/api/sync",
];

function emitConfigChanged(path: string, method: string | undefined) {
  if (!method || method === "GET") return;
  if (!CONFIG_PATHS.some((p) => path === p || path.startsWith(p + "/"))) {
    return;
  }
  for (const fn of configChangeListeners) {
    try {
      fn();
    } catch {
      /* a listener must never break the request */
    }
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
  emitConfigChanged(path, init?.method);
  return data as T;
}

export interface CertList {
  active: CertInfo | null;
  versions: CertInfo[];
}

export interface AgentInbound {
  name: string;
  protocol: string;
  port: number;
  source: string;
  listening?: boolean;
}

export interface AgentSystem {
  cpuPct: number;
  load1: number;
  load5: number;
  load15: number;
  memTotal: number;
  memUsed: number;
  memPct: number;
  diskTotal: number;
  diskUsed: number;
  diskPct: number;
  available: boolean;
}

export interface AgentTrafficEntry {
  inbound: string;
  user?: string;
  up: number;
  down: number;
}

export interface AgentTraffic {
  up: number;
  down: number;
  unassignedUp: number;
  unassignedDown: number;
  byInbound: AgentTrafficEntry[];
  byUser: AgentTrafficEntry[];
  updatedAt: number;
}

export interface TrafficSummary {
  local: AgentTraffic;
  nodes: { nodeId: number; nodeName: string; traffic: AgentTraffic }[];
  bySubscription: {
    name: string;
    up: number;
    down: number;
    byInbound: { inbound: string; node: string; up: number; down: number }[];
  }[];
  byInbound: { inbound: string; node: string; up: number; down: number }[];
  unassignedUp: number;
  unassignedDown: number;
}

export interface NodeView {
  id: number;
  name: string;
  address: string;
  publicHost: string;
  tlsMode: string;
  usageRatio: number;
  paused: boolean;
  pausePending: boolean;
  syncStatus: string;
  syncDetail: string;
  agentUnitActive: string;
  agentClashAlive: boolean;
  agentStatusAt: number;
  agentInbounds: AgentInbound[];
  agentSystem?: AgentSystem;
  agentTraffic?: AgentTraffic;
  agentLastResult: string;
  agentBlockedHash: string;
  online: boolean;
  lastPushAt: number;
  lastPushResult: string;
  lastError: string;
  lastOnlineAt: number;
  lastInboundCount: number;
  lastVersion: string;
  warning?: string;
  createdAt: number;
  updatedAt: number;
}

export interface NodeDetail {
  node: NodeView;
  inboundIds: number[];
}

// Panel host identity for the inbound form's 本机 row (GET /api/local-machine).
export interface LocalMachine {
  hostname: string;
  ip: string;
  port: number;
}

export interface NodeCreate {
  name: string;
  address: string;
  token?: string;
  pairing_code?: string;
  publicHost?: string;
  tlsMode?: string;
  usageRatio?: number;
  addToAllInbounds?: boolean;
}

export interface NodeUpdate {
  name?: string;
  address?: string;
  token?: string;
  pairing_code?: string;
  publicHost?: string;
  tlsMode?: string;
  usageRatio?: number;
  paused?: boolean;
}

export interface AgentStatusInbound {
  name: string;
  protocol: string;
  port: number;
  source: string;
  listening?: boolean;
}

export interface AgentStatus {
  unitActive: string;
  clashAlive: boolean;
  version?: string;
  lastHash: string;
  lastSyncAt: number;
  lastResult: string;
  blockedHash?: string;
  inbounds: AgentStatusInbound[];
}

// One node core inbound offered for import. Params are never sent:
// confirm re-fetches from the agent by name, so import secrets stay
// off the browser for this flow.
export interface ImportCandidate {
  name: string;
  protocol: string;
  port: number;
  disabledReason?: string;
}

export interface ImportCreated {
  id: number;
  name: string;
  protocol: string;
  port: number;
}

export interface ImportSkipped {
  name: string;
  reason: string;
}

export interface ImportResult {
  created: ImportCreated[];
  skipped: ImportSkipped[];
}

export const api = {
  status: () => request<AuthStatus>("/api/auth/status"),
  login: (username: string, password: string) =>
    request<{ username: string }>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ ok: boolean }>("/api/auth/logout", { method: "POST" }),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ ok: boolean }>("/api/auth/password", {
      method: "PATCH",
      body: JSON.stringify({ currentPassword, newPassword }),
    }),
  changeUsername: (username: string, currentPassword: string) =>
    request<{ username: string }>("/api/auth/username", {
      method: "PATCH",
      body: JSON.stringify({ username, currentPassword }),
    }),
  changeAccount: (body: {
    username?: string;
    currentPassword: string;
    newPassword?: string;
  }) =>
    request<{ username: string }>("/api/auth/account", {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
  revokeOtherSessions: () =>
    request<{ revoked: number }>("/api/auth/sessions/revoke-others", {
      method: "POST",
    }),
  dashboardSummary: () =>
    request<DashboardSummary>("/api/dashboard/summary"),
  localMachine: () => request<LocalMachine>("/api/local-machine"),
  logs: (params: LogQuery) => {
    const qs = new URLSearchParams();
    qs.set("category", params.category);
    if (params.q) qs.set("q", params.q);
    if (params.level) qs.set("level", params.level);
    if (params.result) qs.set("result", params.result);
    if (params.before) qs.set("before", params.before);
    qs.set("limit", String(params.limit ?? 100));
    return request<LogPage>(`/api/logs?${qs.toString()}`);
  },
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
    rename: (id: string, note: string) =>
      request<CertInfo>(`/api/certs/${encodeURIComponent(id)}/note`, {
        method: "PATCH",
        body: JSON.stringify({ note }),
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
  inbounds: {
    list: () => request<InboundList>("/api/inbounds"),
    create: (body: InboundCreate) =>
      request<InboundView>("/api/inbounds", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: InboundUpdate) =>
      request<InboundView>(`/api/inbounds/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean }>(`/api/inbounds/${id}`, { method: "DELETE" }),
    setEnabled: (id: number, enabled: boolean) =>
      request<InboundView>(`/api/inbounds/${id}/enabled`, {
        method: "PATCH",
        body: JSON.stringify({ enabled }),
      }),
    keypair: () =>
      request<RealityKeypair>("/api/inbounds/reality-keypair", {
        method: "POST",
      }),
  },
  trafficSummary: () => request<TrafficSummary>("/api/traffic/summary"),
  nodes: {
    list: () => request<NodeView[]>("/api/nodes"),
    get: (id: number) => request<NodeDetail>(`/api/nodes/${id}`),
    create: (body: NodeCreate) =>
      request<NodeView>("/api/nodes", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: NodeUpdate) =>
      request<NodeView>(`/api/nodes/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean; warning: string }>(`/api/nodes/${id}`, {
        method: "DELETE",
      }),
    push: (id: number) =>
      request<{ started: boolean; busy: boolean }>(
        `/api/nodes/${id}/push`,
        { method: "POST" },
      ),
    clearBlock: (id: number) =>
      request<{ cleared: boolean; pushStarted: boolean }>(
        `/api/nodes/${id}/clear-block`,
        { method: "POST" },
      ),
    probeStatus: (id: number) =>
      request<{ status: AgentStatus; node: NodeView }>(
        `/api/nodes/${id}/status`,
      ),
    getInboundNodes: (inboundId: number) =>
      request<{ nodeIds: number[]; nodes: InboundNodeAssignment[] }>(
        `/api/inbounds/${inboundId}/nodes`,
      ),
    setInboundNodes: (inboundId: number, nodes: InboundNodeAssignment[]) =>
      request<{ nodeIds: number[]; nodes: InboundNodeAssignment[] }>(
        `/api/inbounds/${inboundId}/nodes`,
        {
          method: "PATCH",
          body: JSON.stringify({ nodes }),
        },
      ),
    checkPort: (nodeId: number, port: number, transport: string) =>
      request<AgentPortCheck>(
        `/api/nodes/${nodeId}/check-port?port=${port}&transport=${encodeURIComponent(transport)}`,
      ),
    requestPairingCode: (address: string) =>
      request<{ ok: boolean; retry_after: number }>(
        `/api/nodes/request-pairing-code`,
        {
          method: "POST",
          body: JSON.stringify({ address }),
        },
      ),
    importScan: (nodeId: number) =>
      request<{ candidates: ImportCandidate[] }>(
        `/api/nodes/${nodeId}/import-scan`,
      ),
    importInbounds: (nodeId: number, names: string[]) =>
      request<ImportResult>(`/api/nodes/${nodeId}/import`, {
        method: "POST",
        body: JSON.stringify({ names }),
      }),
  },
  templates: {
    list: () => request<TemplateView[]>("/api/templates"),
    create: (body: TemplateRequest) =>
      request<TemplateView>("/api/templates", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: TemplateRequest) =>
      request<TemplateView>(`/api/templates/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean }>(`/api/templates/${id}`, { method: "DELETE" }),
  },
  subscriptions: {
    list: () => request<SubscriptionView[]>("/api/subscriptions"),
    create: (body: SubscriptionRequest) =>
      request<SubscriptionView>("/api/subscriptions", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    update: (id: number, body: SubscriptionRequest) =>
      request<SubscriptionView>(`/api/subscriptions/${id}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    remove: (id: number) =>
      request<{ ok: boolean }>(`/api/subscriptions/${id}`, {
        method: "DELETE",
      }),
    setEnabled: (id: number, enabled: boolean) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/enabled`, {
        method: "PATCH",
        body: JSON.stringify({ enabled }),
      }),
    resetToken: (id: number) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/reset-token`, {
        method: "POST",
      }),
    resetUuid: (id: number) =>
      request<SubscriptionView>(`/api/subscriptions/${id}/reset-uuid`, {
        method: "POST",
      }),
    preview: (id: number) =>
      request<SubscriptionPreview>(`/api/subscriptions/${id}/preview`),
    onlineIps: () =>
      request<{
        counts: Record<string, number>;
        total: number;
      }>("/api/subscriptions/online-ips"),
  },
  settings: {
    get: () => request<SettingsView>("/api/settings"),
    update: (body: SettingsUpdate) =>
      request<SettingsView>("/api/settings", {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
  },
  sync: {
    status: () => request<SyncStatus>("/api/sync/status"),
    run: () => request<SyncLogEntry>("/api/sync", { method: "POST" }),
  },
};

export interface SyncLogEntry {
  id: number;
  at: number;
  action: string;
  result: string;
  hash: string;
  detail: string;
}

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

export interface SyncStatus {
  targetPath: string;
  restartEnabled: boolean;
  desiredHash: string;
  targetHash: string;
  inSync: boolean;
  last: SyncLogEntry | null;
}

export interface SubscriptionPreview {
  format: string;
  contentType: string;
  content: string;
}

export interface SettingsView {
  webrtcMode: "proxy" | "reject" | "off";
  dnsFallback: "foreign" | "domestic";
  dnsRespectRules: boolean;
  linkFormat: "clash" | "singbox";
  skipCertVerify: boolean;
  publicHost: string;
  showUserinfo: boolean;
  updateIntervalHours: number;
  textSelectable: boolean;
  timezone: string;
  showWarnings: boolean;
  dashIntervalSecs: number;
  autoSyncEnabled: boolean;
  autoSyncIntervalSecs: number;
}

export interface SettingsUpdate {
  webrtcMode?: SettingsView["webrtcMode"];
  dnsFallback?: SettingsView["dnsFallback"];
  dnsRespectRules?: boolean;
  linkFormat?: SettingsView["linkFormat"];
  skipCertVerify?: boolean;
  publicHost?: string;
  showUserinfo?: boolean;
  updateIntervalHours?: number;
  textSelectable?: boolean;
  timezone?: string;
  showWarnings?: boolean;
  dashIntervalSecs?: number;
  autoSyncEnabled?: boolean;
  autoSyncIntervalSecs?: number;
}

export interface TemplateGroup {
  name: string;
  type: string;
  inboundIds: number[];
  enabled: boolean;
  url?: string;
  interval?: number;
  tolerance?: number;
}

export interface TemplateView {
  id: number;
  name: string;
  inboundIds: number[];
  groups: TemplateGroup[];
  subscriptionCount: number;
  createdAt: number;
  updatedAt: number;
}

export interface TemplateRequest {
  name: string;
  inboundIds: number[];
  groups?: TemplateGroup[];
}

export type SubscriptionStatus =
  | "active"
  | "disabled"
  | "expired"
  | "over_quota";

export interface SubscriptionView {
  id: number;
  name: string;
  uuid: string;
  token: string;
  templateId: number;
  templateName: string;
  limitBytes: number;
  usedUp: number;
  usedDown: number;
  usedBytes: number;
  expireAt: number;
  maxIps: number;
  enabled: boolean;
  status: SubscriptionStatus;
  createdAt: number;
  updatedAt: number;
}

export interface SubscriptionRequest {
  name: string;
  templateId: number;
  limitBytes: number;
  expireAt: number;
  maxIps: number;
}

export interface RealityParams {
  sni: string;
  publicKey: string;
  privateKey: string;
  shortId: string;
  flow: string;
}

export interface Hy2Params {
  upMbps: number;
  downMbps: number;
  password: string;
  obfs: string;
  obfsPassword: string;
}

export interface TuicParams {
  uuid: string;
  password: string;
  congestionControl: string;
  sni: string;
}

export interface AnyTLSParams {
  password: string;
  sni: string;
}

export interface TrojanParams {
  password: string;
  sni: string;
}

export type InboundProtocol =
  | "vless-reality"
  | "hysteria2"
  | "tuic"
  | "anytls"
  | "trojan";

export type InboundParams =
  | RealityParams
  | Hy2Params
  | TuicParams
  | AnyTLSParams
  | TrojanParams;

export type InboundStatus =
  | "synced"
  | "drifted"
  | "pending"
  | "unknown"
  | "disabled"
  | "not_listening"
  | "paused"
  | "offline";

export interface InboundDiff {
  field:
    | "sni"
    | "publicKey"
    | "shortId"
    | "upMbps"
    | "downMbps"
    | "uuid"
    | "congestion"
    | "method";
  panel: string;
  running: string;
}

export interface InboundView {
  id: number;
  name: string;
  protocol: InboundProtocol;
  port: number;
  params: InboundParams;
  enabled: boolean;
  runLocal: boolean;
  status: InboundStatus;
  diffs?: InboundDiff[];
  createdAt: number;
  updatedAt: number;
}

export interface RunningInbound {
  tag: string;
  type: string;
  port: number;
  sni?: string;
  publicKey?: string;
  shortId?: string;
  upMbps?: number;
  downMbps?: number;
}

export interface InboundList {
  items: InboundView[];
  runningAvailable: boolean;
  extras: RunningInbound[];
}

export interface InboundCreate {
  name: string;
  protocol: string;
  port: number;
  params: InboundParams;
  runLocal?: boolean;
}

export interface InboundUpdate {
  name: string;
  port: number;
  params: InboundParams;
  runLocal?: boolean;
}

// One inbound -> node assignment. Port 0 = the inbound's own port.
export interface InboundNodeAssignment {
  nodeId: number;
  port: number;
}

export interface AgentPortCheck {
  port: number;
  transport: string;
  occupied: boolean;
  reason: string;
  kind: string;
  occupiedBy: string;
}

export interface RealityKeypair {
  publicKey: string;
  privateKey: string;
  shortId: string;
}
