import { request } from "./client";

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

export interface RealityKeypair {
  publicKey: string;
  privateKey: string;
  shortId: string;
}


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

export const inboundsApi = {
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
};
