import type { InboundDiff, InboundProtocol } from "@/lib/api";

export const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// 本机 dropdown label, mirroring the node rows' name（address） format.
// Partial data still beats the bare fallback: hostname alone, or
// hostname（ip）, when the port or IP is unavailable.

export const DIFF_FIELD_LABEL: Record<InboundDiff["field"], string> = {
  sni: "SNI",
  publicKey: "公钥",
  shortId: "Short ID",
  upMbps: "上行 Mbps",
  downMbps: "下行 Mbps",
  uuid: "UUID",
  congestion: "拥塞控制",
  method: "加密方法",
};

export const PROTOCOL_LABEL: Record<InboundProtocol, string> = {
  "vless-reality": "VLESS · Reality",
  hysteria2: "Hysteria2",
  tuic: "TUIC v5",
  anytls: "AnyTLS",
  trojan: "Trojan",
};

// 传输类型（TCP / UDP）由协议固定，跟后端 protocolSpecs 的 transport 一致。

export const PROTOCOL_TRANSPORT: Record<InboundProtocol, string> = {
  "vless-reality": "TCP",
  hysteria2: "UDP",
  tuic: "UDP",
  anytls: "TCP",
  trojan: "TCP",
};

// 入站实际运行的机器：本机或某个节点，含该机器上的端口。

export interface MachineInfo {
  name: string;
  ip: string;
  port: number;
}

export const DEFAULT_PORT: Record<InboundProtocol, number> = {
  "vless-reality": 443,
  hysteria2: 8443,
  tuic: 443,
  anytls: 8443,
  trojan: 443,
};


// Fixed option values for the form's Select dropdowns. These must match
// the SelectItem values rendered below; customSelectOption uses them
// only to decide whether the current value needs a temporary option.

export const OBFS_OPTIONS = ["none", "salamander"] as const;
export const TUIC_CONGESTION_OPTIONS = ["bbr", "cubic", "new_reno"] as const;
