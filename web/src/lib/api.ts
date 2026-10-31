import { authApi } from "./api/auth";
import { certsApi } from "./api/certs";
import { dashboardSummary, dashboardTrend, localMachine } from "./api/dashboard";
import { inboundsApi } from "./api/inbounds";
import { logsApi } from "./api/logs";
import { nodesApi } from "./api/nodes";
import { settingsApi } from "./api/settings";
import { subscriptionsApi } from "./api/subscriptions";
import { syncApi } from "./api/sync";
import { templatesApi } from "./api/templates";
import { trafficSummary } from "./api/traffic";

export { ApiError, onConfigChanged } from "./api/client";
export type { AuthStatus } from "./api/auth";
export type {
  CertInfo,
  CertList,
} from "./api/certs";
export type {
  DashboardSample,
  DashboardSummary,
  DashboardTrend,
  SystemInfo,
  TrafficInfo,
  TrendGranularity,
  TrendPoint,
} from "./api/dashboard";
export type {
  AgentInbound,
  AgentSystem,
  AgentTraffic,
  AgentTrafficEntry,
  TrafficSummary,
} from "./api/traffic";
export type {
  AgentStatus,
  AgentStatusInbound,
  ImportCandidate,
  ImportCreated,
  ImportResult,
  ImportSkipped,
  LocalMachine,
  NodeCreate,
  NodeDetail,
  NodeUpdate,
  NodeView,
} from "./api/nodes";
export type {
  LogCategory,
  LogEntry,
  LogPage,
  LogQuery,
} from "./api/logs";
export type {
  SyncLogEntry,
  SyncStatus,
} from "./api/sync";
export type {
  TemplateGroup,
  TemplateRequest,
  TemplateView,
} from "./api/templates";
export type {
  SubscriptionPreview,
  SubscriptionRequest,
  SubscriptionStatus,
  SubscriptionView,
} from "./api/subscriptions";
export type {
  SettingsUpdate,
  SettingsView,
} from "./api/settings";
export type {
  AgentPortCheck,
  AnyTLSParams,
  Hy2Params,
  InboundCreate,
  InboundDiff,
  InboundNodeAssignment,
  InboundList,
  InboundParams,
  InboundProtocol,
  InboundStatus,
  InboundUpdate,
  InboundView,
  RealityKeypair,
  RealityParams,
  RunningInbound,
  TrojanParams,
  TuicParams,
} from "./api/inbounds";

export const api = {
  ...authApi,
  dashboardSummary,
  localMachine,
  logs: logsApi,
  dashboardTrend,
  certs: certsApi,
  inbounds: inboundsApi,
  trafficSummary,
  nodes: nodesApi,
  templates: templatesApi,
  subscriptions: subscriptionsApi,
  settings: settingsApi,
  sync: syncApi,
};
