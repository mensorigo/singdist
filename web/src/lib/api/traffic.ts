import { request } from "./client";

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


export const trafficSummary = () => request<TrafficSummary>("/api/traffic/summary");
