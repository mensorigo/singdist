import type { TemplateGroup } from "@/lib/api";

export type GroupType = "select" | "url-test" | "fallback" | "load-balance";

export const GROUP_TYPE_LABEL: Record<GroupType, string> = {
  select: "手动选择",
  "url-test": "自动选择",
  fallback: "故障转移",
  "load-balance": "负载均衡",
};

export interface GroupDraft {
  id: string;
  name: string;
  type: GroupType;
  nodeIds: number[];
  enabled: boolean;
  url: string;
  interval: number;
  tolerance: number;
}

// 默认组：名字 + 类型与后端默认值一致的组，不能删除、不能改名，
// 但可以编辑节点和开关。自定义组可删可改名。
export function isDefaultGroup(g: GroupDraft): boolean {
  return (
    (g.name === "Proxy" && g.type === "select") ||
    (g.name === "Auto" && g.type === "url-test") ||
    (g.name === "Fallback" && g.type === "fallback")
  );
}

export function toDraft(g: TemplateGroup, fallbackId: string): GroupDraft {
  return {
    id: fallbackId,
    name: g.name,
    type: (g.type as GroupType) ?? "select",
    nodeIds: g.inboundIds ?? [],
    enabled: g.enabled !== false,
    url: g.url ?? "https://www.gstatic.com/generate_204",
    interval: g.interval ?? 300,
    tolerance: g.tolerance ?? 50,
  };
}

export function toApiGroup(g: GroupDraft): TemplateGroup {
  const out: TemplateGroup = {
    name: g.name,
    type: g.type,
    inboundIds: g.nodeIds,
    enabled: g.enabled,
  };
  if (g.type !== "select") {
    out.url = g.url || "https://www.gstatic.com/generate_204";
    out.interval = g.interval > 0 ? g.interval : 300;
    out.tolerance = g.tolerance >= 0 ? g.tolerance : 50;
  }
  return out;
}
