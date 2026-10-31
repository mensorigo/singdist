import { request } from "./client";
import type { AgentPortCheck, InboundNodeAssignment } from "./inbounds";
import type { AgentInbound, AgentSystem, AgentTraffic } from "./traffic";

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


export const nodesApi = {
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
};
