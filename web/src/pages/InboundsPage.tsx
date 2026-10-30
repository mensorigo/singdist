import { useCallback, useEffect, useState } from "react";
import {
  Check,
  Copy,
  Eye,
  EyeOff,
  Pencil,
  Trash,
} from "lucide-react";
import { toast } from "sonner";

import { ConfirmDialog } from "@/components/confirm-dialog";
import { EnabledSwitch } from "@/components/enabled-switch";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  api,
  ApiError,
  type AnyTLSParams,
  type Hy2Params,
  type InboundDiff,
  type InboundList,
  type InboundNodeAssignment,
  type InboundParams,
  type InboundProtocol,
  type InboundStatus,
  type InboundView,
  type LocalMachine,
  type NodeView,
  type RealityParams,
  type ShadowsocksParams,
  type TrojanParams,
  type TuicParams,
} from "@/lib/api";
import { customSelectOption } from "@/lib/custom-option";
import { cn } from "@/lib/utils";
import TemplatesPanel from "@/pages/TemplatesPanel";

function randomHex(bytes: number): string {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  return Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
}

// Row action trigger style, verbatim mirror of the SubscriptionsPage
// RowActions baseline (the screenshot style): borderless 32px icon
// button, muted icon, muted hover background. Disabled triggers add
// opacity-50 for affordance.
const ICON_BTN_CLS =
  "flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground";

// 本机 dropdown label, mirroring the node rows' name（address） format.
// Partial data still beats the bare fallback: hostname alone, or
// hostname（ip）, when the port or IP is unavailable.
function formatLocalLabel(lm: LocalMachine): string {
  if (!lm.hostname) return "本机";
  if (lm.ip && lm.port > 0) return `${lm.hostname}（${lm.ip}:${lm.port}）`;
  if (lm.ip) return `${lm.hostname}（${lm.ip}）`;
  return lm.hostname;
}

function StatusBadge({
  status,
  showWarnings,
}: {
  status: InboundStatus;
  showWarnings: boolean;
}) {
  const map = {
    synced: ["已同步", "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"],
    // Warnings off: the drifted badge keeps its label but drops the
    // yellow, so no amber remains anywhere on the page.
    drifted: [
      "有差异",
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    pending: ["未同步", "bg-muted text-muted-foreground"],
    unknown: ["无法对照", "bg-muted text-muted-foreground"],
    disabled: ["已停用", "bg-muted text-muted-foreground"],
    not_listening: [
      "未监听",
      showWarnings
        ? "bg-amber-500/10 text-amber-600 dark:text-amber-400"
        : "bg-muted text-muted-foreground",
    ],
    paused: ["已暂停", "bg-amber-500/10 text-amber-600 dark:text-amber-400"],
    offline: ["离线", "bg-muted text-muted-foreground"],
  } as const;
  const [label, cls] = map[status];
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2 py-0.5 text-xs font-medium",
        cls,
      )}
    >
      {label}
    </span>
  );
}

const DIFF_FIELD_LABEL: Record<InboundDiff["field"], string> = {
  sni: "SNI",
  publicKey: "公钥",
  shortId: "Short ID",
  upMbps: "上行 Mbps",
  downMbps: "下行 Mbps",
  uuid: "UUID",
  congestion: "拥塞控制",
  method: "加密方法",
};

const PROTOCOL_LABEL: Record<InboundProtocol, string> = {
  "vless-reality": "VLESS · Reality",
  hysteria2: "Hysteria2",
  tuic: "TUIC v5",
  shadowsocks: "Shadowsocks",
  anytls: "AnyTLS",
  trojan: "Trojan",
};

// 传输类型（TCP / UDP）由协议固定，跟后端 protocolSpecs 的 transport 一致。
const PROTOCOL_TRANSPORT: Record<InboundProtocol, string> = {
  "vless-reality": "TCP",
  hysteria2: "UDP",
  tuic: "UDP",
  shadowsocks: "TCP+UDP",
  anytls: "TCP",
  trojan: "TCP",
};

// 入站实际运行的机器：本机或某个节点，含该机器上的端口。
interface MachineInfo {
  name: string;
  ip: string;
  port: number;
}

const DEFAULT_PORT: Record<InboundProtocol, number> = {
  "vless-reality": 443,
  hysteria2: 8443,
  tuic: 443,
  shadowsocks: 8388,
  anytls: 8443,
  trojan: 443,
};

// Key lengths for the Shadowsocks 2022 methods; other methods take a
// free-form password.
const SS2022_KEY_LEN: Record<string, number> = {
  "2022-blake3-aes-128-gcm": 16,
  "2022-blake3-aes-256-gcm": 32,
  "2022-blake3-chacha20-poly1305": 32,
};

// Fixed option values for the form's Select dropdowns. These must match
// the SelectItem values rendered below; customSelectOption uses them
// only to decide whether the current value needs a temporary option.
const OBFS_OPTIONS = ["none", "salamander"] as const;
const TUIC_CONGESTION_OPTIONS = ["bbr", "cubic", "new_reno"] as const;
const SS_METHOD_OPTIONS = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
] as const;

function randomBase64(bytes: number): string {
  const a = new Uint8Array(bytes);
  crypto.getRandomValues(a);
  let s = "";
  a.forEach((b) => {
    s += String.fromCharCode(b);
  });
  return btoa(s);
}

function shortVal(field: InboundDiff["field"], v: string): string {
  if (field === "publicKey" && v.length > 14) return `${v.slice(0, 10)}…`;
  return v === "" ? "（空）" : v;
}

// DiffsList renders the field-level panel-vs-running differences.
function DiffsList({ diffs }: { diffs: InboundDiff[] }) {
  return (
    <ul className="mt-1.5 space-y-0.5 text-[11px] leading-snug text-amber-700 dark:text-amber-300">
      {diffs.map((d) => (
        <li key={d.field} className="font-mono">
          {DIFF_FIELD_LABEL[d.field]}：面板 {shortVal(d.field, d.panel)} ·
          运行 {shortVal(d.field, d.running)}
        </li>
      ))}
    </ul>
  );
}

// SecretInput is a mono input masked by default, with a show/hide toggle.
function SecretInput({
  id,
  value,
  onChange,
  placeholder,
}: {
  id: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <Input
        id={id}
        type={show ? "text" : "password"}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoComplete="off"
        className="pr-9 font-mono text-xs"
      />
      <button
        type="button"
        aria-label={show ? "隐藏" : "显示"}
        onClick={() => setShow((s) => !s)}
        className="absolute right-1 top-1/2 flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground"
      >
        {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  );
}

export default function InboundsPage({
  refreshSignal,
  showWarnings,
}: {
  refreshSignal: number;
  showWarnings: boolean;
}) {
  const [tab, setTab] = useState<"inbounds" | "templates">("inbounds");
  const [list, setList] = useState<InboundList | null>(null);
  const [machines, setMachines] = useState<Record<number, MachineInfo>>({});
  const [loadError, setLoadError] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [dialogItem, setDialogItem] = useState<InboundView | "new" | null>(
    null,
  );
  const [pendingDelete, setPendingDelete] = useState<InboundView | null>(
    null,
  );

  const refresh = useCallback(async () => {
    try {
      const data = await api.inbounds.list();
      setList(data);
      setLoadError(false);
      // 运行机器信息：本机取 local-machine，节点取节点列表 + 入站分配。
      // 机器信息拿不到时列表照常显示，对应格子显示“—”。
      void (async () => {
        try {
          const [lm, nodeList] = await Promise.all([
            api.localMachine(),
            api.nodes.list(),
          ]);
          const byId = new Map(nodeList.map((n) => [n.id, n]));
          const info: Record<number, MachineInfo> = {};
          await Promise.all(
            data.items.map(async (v) => {
              if (v.runLocal) {
                info[v.id] = {
                  name: lm.hostname || "本机",
                  ip: lm.ip || "—",
                  port: v.port,
                };
                return;
              }
              try {
                const res = await api.nodes.getInboundNodes(v.id);
                const first = res.nodes?.[0];
                const node = first ? byId.get(first.nodeId) : undefined;
                if (node) {
                  const host = (node.publicHost || node.address || "").split(
                    ":",
                  )[0];
                  info[v.id] = {
                    name: node.name || "—",
                    ip: host || "—",
                    port: first.port > 0 ? first.port : v.port,
                  };
                }
              } catch {
                // 节点信息拿不到时该行机器格留空，不影响列表本身。
              }
            }),
          );
          setMachines(info);
        } catch {
          // 机器信息整体拿不到时列表照常显示。
        }
      })();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        window.location.reload();
        return;
      }
      setLoadError(true);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh, refreshSignal]);

  const confirmDelete = () => {
    if (!pendingDelete) return;
    const v = pendingDelete;
    setPendingDelete(null);
    setActionError(null);
    setBusyId(v.id);
    void (async () => {
      try {
        await api.inbounds.remove(v.id);
        await refresh();
        toast.success("入站已删除");
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : "删除失败，请重试",
        );
      } finally {
        setBusyId(null);
      }
    })();
  };

  const toggleEnabled = (v: InboundView) => {
    setActionError(null);
    setBusyId(v.id);
    void (async () => {
      try {
        await api.inbounds.setEnabled(v.id, !v.enabled);
        await refresh();
        toast.success(v.enabled ? "入站已停用" : "入站已启用");
      } catch (err) {
        setActionError(
          err instanceof ApiError ? err.message : "操作失败，请重试",
        );
      } finally {
        setBusyId(null);
      }
    })();
  };

  const tabSwitch = (
    <div className="inline-flex rounded-xl bg-muted p-1">
      {(
        [
          ["inbounds", "入站"],
          ["templates", "模板"],
        ] as const
      ).map(([key, label]) => (
        <button
          key={key}
          type="button"
          onClick={() => setTab(key)}
          className={cn(
            "h-7 rounded-lg px-3.5 text-sm transition-colors",
            tab === key
              ? "bg-background font-medium shadow-sm"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {label}
        </button>
      ))}
    </div>
  );

  if (!list) {
    return (
      <div className="space-y-4">
        {tabSwitch}
        {tab === "templates" ? (
          <TemplatesPanel refreshSignal={refreshSignal} />
        ) : (
          <p className="text-sm text-muted-foreground">
            {loadError ? "暂时无法获取入站数据，请稍后刷新重试" : "数据加载中…"}
          </p>
        )}
      </div>
    );
  }

  const outOfSync = list.items.filter(
    (v) =>
      v.status === "pending" ||
      v.status === "drifted" ||
      v.status === "not_listening",
  ).length;

  return (
    <div className="space-y-4">
      {tabSwitch}
      {tab === "templates" ? (
        <TemplatesPanel refreshSignal={refreshSignal} />
      ) : (
        <>
      {showWarnings && !list.runningAvailable && (
        <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-[13px] text-amber-700 dark:text-amber-300">
          无法读取核心的运行配置，本页仅显示面板记录，同步状态不可用。
        </div>
      )}
      {showWarnings && list.runningAvailable && outOfSync > 0 && (
        <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-[13px] text-amber-700 dark:text-amber-300">
          有 {outOfSync} 个入站与运行配置不一致 — 面板记录是期望状态，点顶栏的同步图标应用。
        </div>
      )}
      {list.extras.length > 0 && (
        <div className="rounded-[18px] border bg-card px-4 py-3 text-[13px] text-muted-foreground shadow-sm">
          核心正在运行、但面板尚未纳管的入站：
          {list.extras
            .map((e) => `${e.tag || e.type}（${e.type}:${e.port}）`)
            .join("、")}
        </div>
      )}

      <section className="rounded-[18px] border bg-card px-6 py-6 text-card-foreground shadow-sm">
        <div className="flex items-center justify-between">
          <p className="text-base font-semibold">入站列表</p>
          <Button
            onClick={() => setDialogItem("new")}
            className="h-8 rounded-xl px-3"
          >
            新建入站
          </Button>
        </div>
        {actionError && (
          <p className="mt-2 text-[13px] text-destructive">{actionError}</p>
        )}
        <div className="mt-3 overflow-x-auto">
          <table className="w-full min-w-[920px] text-sm">
            <thead>
              <tr className="border-b text-left text-xs text-muted-foreground">
                <th className="py-2 pr-3 font-medium">名称</th>
                <th className="py-2 pr-3 font-medium">运行机器</th>
                <th className="py-2 pr-3 font-medium">机器IP</th>
                <th className="py-2 pr-3 font-medium">端口</th>
                <th className="py-2 pr-3 font-medium">协议</th>
                <th className="py-2 pr-3 font-medium">传输</th>
                <th className="py-2 pr-3 font-medium">状态</th>
                <th className="py-2 font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((v) => (
                <tr key={v.id} className="border-b last:border-0">
                  <td
                    className={cn(
                      "py-2.5 pr-3 font-medium",
                      !v.enabled && "text-muted-foreground",
                    )}
                  >
                    {v.name}
                  </td>
                  <td className="py-2.5 pr-3">
                    {machines[v.id]?.name ?? (v.runLocal ? "本机" : "—")}
                  </td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {machines[v.id]?.ip ?? "—"}
                  </td>
                  <td className="py-2.5 pr-3 tabular-nums">
                    {machines[v.id]?.port ?? v.port}
                  </td>
                  <td className="py-2.5 pr-3">
                    <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {PROTOCOL_LABEL[v.protocol]}
                    </span>
                  </td>
                  <td className="py-2.5 pr-3">
                    <span className="inline-flex rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
                      {PROTOCOL_TRANSPORT[v.protocol]}
                    </span>
                  </td>
                  <td className="py-2.5 pr-3">
                    <StatusBadge
                      status={v.status}
                      showWarnings={showWarnings}
                    />
                    {showWarnings &&
                      v.status === "drifted" &&
                      v.diffs &&
                      v.diffs.length > 0 && <DiffsList diffs={v.diffs} />}
                  </td>
                  <td className="py-2.5">
                    <div className="flex items-center gap-3">
                      <EnabledSwitch
                        enabled={v.enabled}
                        disabled={busyId === v.id}
                        onToggle={() => toggleEnabled(v)}
                      />
                      <button
                        type="button"
                        title="编辑"
                        aria-label="编辑"
                        disabled={busyId === v.id}
                        onClick={() => setDialogItem(v)}
                        className={`${ICON_BTN_CLS} disabled:opacity-50`}
                      >
                        <Pencil className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        title="删除"
                        aria-label="删除"
                        disabled={busyId === v.id}
                        onClick={() => setPendingDelete(v)}
                        className={`${ICON_BTN_CLS} disabled:opacity-50`}
                      >
                        <Trash className="h-4 w-4" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {list.items.length === 0 && (
                <tr>
                  <td
                    colSpan={8}
                    className="py-6 text-center text-sm text-muted-foreground"
                  >
                    暂无入站，点右上角「新建入站」创建
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
      </>
      )}

      {dialogItem !== null && (
        <InboundFormDialog
          item={dialogItem === "new" ? null : dialogItem}
          showWarnings={showWarnings}
          onClose={() => setDialogItem(null)}
          onSaved={async () => {
            setDialogItem(null);
            await refresh();
          }}
        />
      )}

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title="删除入站"
        description={
          pendingDelete
            ? `确定要删除入站「${pendingDelete.name}」吗？此操作只删除面板记录，核心配置会在下次同步时移除该入站。`
            : undefined
        }
        confirmLabel="删除"
        destructive
        onConfirm={confirmDelete}
      />
    </div>
  );
}

// --- create / edit form -----------------------------------------------------

function InboundFormDialog({
  item,
  onClose,
  onSaved,
  showWarnings,
}: {
  item: InboundView | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  showWarnings: boolean;
}) {
  const creating = item === null;
  const [protocol, setProtocol] = useState<InboundProtocol>(
    item?.protocol ?? "vless-reality",
  );
  const [name, setName] = useState(item?.name ?? "");
  const [port, setPort] = useState(item?.port ?? 443);

  const rp = item?.protocol === "vless-reality" ? (item.params as RealityParams) : null;
  const [sni, setSni] = useState(rp?.sni ?? "");
  const [flow, setFlow] = useState(rp?.flow ?? "xtls-rprx-vision");
  const [publicKey, setPublicKey] = useState(rp?.publicKey ?? "");
  const [privateKey, setPrivateKey] = useState(rp?.privateKey ?? "");
  const [shortId, setShortId] = useState(rp?.shortId ?? "");

  const hp = item?.protocol === "hysteria2" ? (item.params as Hy2Params) : null;
  const [upMbps, setUpMbps] = useState(hp?.upMbps ?? 0);
  const [downMbps, setDownMbps] = useState(hp?.downMbps ?? 0);
  const [password, setPassword] = useState(hp?.password ?? "");
  const [obfs, setObfs] = useState(hp?.obfs ?? "");
  const [obfsPassword, setObfsPassword] = useState(hp?.obfsPassword ?? "");

  const tp = item?.protocol === "tuic" ? (item.params as TuicParams) : null;
  const [tuicUuid, setTuicUuid] = useState(tp?.uuid ?? "");
  const [tuicPassword, setTuicPassword] = useState(tp?.password ?? "");
  const [tuicCongestion, setTuicCongestion] = useState(
    tp?.congestionControl ?? "bbr",
  );
  const [tuicSni, setTuicSni] = useState(tp?.sni ?? "");

  const sp =
    item?.protocol === "shadowsocks"
      ? (item.params as ShadowsocksParams)
      : null;
  const [ssMethod, setSsMethod] = useState(
    sp?.method ?? "2022-blake3-aes-128-gcm",
  );
  const [ssPassword, setSsPassword] = useState(sp?.password ?? "");

  // Temporary dropdown options for values outside the fixed lists (e.g.
  // imported from a config file): shown while current, gone once the
  // user picks a standard option.
  const obfsCustom = customSelectOption(
    OBFS_OPTIONS,
    obfs === "" ? "none" : obfs,
  );
  const tuicCongestionCustom = customSelectOption(
    TUIC_CONGESTION_OPTIONS,
    tuicCongestion,
  );
  const ssMethodCustom = customSelectOption(SS_METHOD_OPTIONS, ssMethod);

  const ap = item?.protocol === "anytls" ? (item.params as AnyTLSParams) : null;
  const [alPassword, setAlPassword] = useState(ap?.password ?? "");
  const [alSni, setAlSni] = useState(ap?.sni ?? "");

  const jp = item?.protocol === "trojan" ? (item.params as TrojanParams) : null;
  const [tjPassword, setTjPassword] = useState(jp?.password ?? "");
  const [tjSni, setTjSni] = useState(jp?.sni ?? "");

  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // One inbound runs on exactly ONE machine: 本机 (local) or one node.
  // `machine` is "local", "node:<id>", or "" while the stored value is
  // still being resolved (edit only; "" preserves stored assignments).
  const [nodes, setNodes] = useState<NodeView[]>([]);
  // 本机 row label: hostname + (IP:port) once loaded, plain 本机 until
  // then (and forever if the endpoint fails — the form never breaks).
  const [localLabel, setLocalLabel] = useState("本机");
  const [machine, setMachine] = useState<string>(
    creating ? "local" : item?.runLocal ? "local" : "",
  );
  // Stored assignments (for the multi-machine note + changed detection).
  const [initialAssignments, setInitialAssignments] = useState<
    InboundNodeAssignment[]
  >([]);
  const [assignmentsLoaded, setAssignmentsLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      // Best effort: a failed fetch keeps the plain 本机 label.
      try {
        const lm = await api.localMachine();
        if (!cancelled) setLocalLabel(formatLocalLabel(lm));
      } catch {
        // Keep the fallback label; the form must never break on this.
      }
      try {
        const list = await api.nodes.list();
        if (cancelled) return;
        setNodes(list);
        if (!creating && item) {
          const res = await api.nodes.getInboundNodes(item.id);
          if (cancelled) return;
          const assigns = res.nodes ?? [];
          setInitialAssignments(assigns);
          setAssignmentsLoaded(true);
          if (item.runLocal) {
            setMachine("local");
          } else if (assigns.length > 0) {
            const first = assigns[0].nodeId;
            // Fallback to 本机 when the stored node is gone (e.g. deleted).
            setMachine(
              list.some((n) => n.id === first) ? `node:${first}` : "local",
            );
          } else {
            // No machine stored at all → default to 本机.
            setMachine("local");
          }
        }
      } catch {
        if (!cancelled) setNodes([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [creating, item]);

  const copyPublicKey = async () => {
    try {
      await navigator.clipboard.writeText(publicKey);
      setCopied(true);
      toast.success("公钥已复制");
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setFormError("复制失败，请手动选择公钥文本复制");
    }
  };

  const genKeypair = async () => {
    try {
      const kp = await api.inbounds.keypair();
      setPublicKey(kp.publicKey);
      setPrivateKey(kp.privateKey);
      if (!shortId) setShortId(kp.shortId);
    } catch {
      setFormError("生成密钥失败，请重试");
    }
  };

  const buildParams = (): InboundParams => {
    switch (protocol) {
      case "vless-reality":
        return {
          sni: sni.trim(),
          publicKey: publicKey.trim(),
          privateKey: privateKey.trim(),
          shortId: shortId.trim(),
          flow: flow.trim() || "xtls-rprx-vision",
        } as RealityParams;
      case "hysteria2":
        return {
          upMbps,
          downMbps,
          password: password.trim(),
          obfs,
          // Unknown obfs values keep their password: only an explicit
          // "none" clears it, so an unrecognized value is never
          // silently destroyed by saving the form.
          obfsPassword: obfs === "" ? "" : obfsPassword.trim(),
        } as Hy2Params;
      case "tuic":
        return {
          uuid: tuicUuid.trim(),
          password: tuicPassword.trim(),
          congestionControl: tuicCongestion,
          sni: tuicSni.trim(),
        } as TuicParams;
      case "shadowsocks":
        return {
          method: ssMethod,
          password: ssPassword.trim(),
        } as ShadowsocksParams;
      case "anytls":
        return {
          password: alPassword.trim(),
          sni: alSni.trim(),
        } as AnyTLSParams;
      case "trojan":
        return {
          password: tjPassword.trim(),
          sni: tjSni.trim(),
        } as TrojanParams;
    }
  };

  // Edit only: is the stored inbound on multiple machines? (run_local +
  // assignments, or several assignments.) Saving collapses it to the one
  // machine picked in the dropdown.
  let multiMachine = false;
  if (!creating && item && assignmentsLoaded) {
    multiMachine =
      (item.runLocal && initialAssignments.length > 0) ||
      initialAssignments.length > 1;
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const params = buildParams();
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        setFormError("端口无效（1-65535）");
        return;
      }
      // "" means the stored machine never resolved (fetch failed): keep
      // the stored runLocal and touch no assignments.
      const runLocal =
        machine === "" ? (item?.runLocal ?? true) : machine === "local";
      const nodeId = machine.startsWith("node:")
        ? Number(machine.slice(5))
        : null;
      // The single port box is the port on the one selected machine.
      const assignments: InboundNodeAssignment[] =
        nodeId !== null ? [{ nodeId, port }] : [];
      let inboundId: number;
      if (creating) {
        const created = await api.inbounds.create({
          name: name.trim(),
          protocol,
          port,
          params,
          runLocal,
        });
        inboundId = created.id;
        toast.success("入站已创建");
      } else {
        await api.inbounds.update(item.id, {
          name: name.trim(),
          port,
          params,
          runLocal,
        });
        inboundId = item.id;
        toast.success("入站已保存");
      }
      // Create: save assignments only when a node is picked (local needs none).
      // Edit: save only when the assignment set actually changed, so an
      // unrelated edit never fails on an unreachable node. Stored port 0
      // means "the inbound's port", so it normalizes to item.port.
      // Skipped entirely when assignments never loaded (preserves them).
      if (creating) {
        if (assignments.length > 0) {
          await api.nodes.setInboundNodes(inboundId, assignments);
        }
      } else if (assignmentsLoaded) {
        const normalize = (list: InboundNodeAssignment[]) =>
          new Map(
            list.map((a) => [a.nodeId, a.port > 0 ? a.port : item.port]),
          );
        const oldMap = normalize(initialAssignments);
        const newMap = normalize(assignments);
        const changed =
          oldMap.size !== newMap.size ||
          [...newMap].some(([id, p]) => oldMap.get(id) !== p);
        if (changed) {
          await api.nodes.setInboundNodes(inboundId, assignments);
        }
      }
      await onSaved();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "保存失败，请重试");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{creating ? "新建入站" : "编辑入站"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {showWarnings && !creating && item.status === "drifted" && item.diffs && item.diffs.length > 0 && (
            <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3">
              <p className="text-[13px] font-medium text-amber-700 dark:text-amber-300">
                与运行配置不一致
              </p>
              <DiffsList diffs={item.diffs} />
            </div>
          )}
          {creating && (
            <div className="space-y-2">
              <Label>协议</Label>
              <div className="grid grid-cols-3 gap-2">
                {(
                  Object.keys(PROTOCOL_LABEL) as InboundProtocol[]
                ).map((key) => (
                  <Button
                    key={key}
                    type="button"
                    variant={protocol === key ? "default" : "outline"}
                    onClick={() => {
                      setProtocol(key);
                      setPort(DEFAULT_PORT[key]);
                    }}
                    className="h-8 rounded-xl px-2 text-[13px]"
                  >
                    {PROTOCOL_LABEL[key]}
                  </Button>
                ))}
              </div>
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="inb-name">名称</Label>
            <Input
              id="inb-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="入站节点备注"
              required
            />
          </div>

          <div className="space-y-2">
            <Label>运行机器</Label>
            <Select value={machine} onValueChange={setMachine}>
              <SelectTrigger>
                <SelectValue placeholder="选择机器" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">{localLabel}</SelectItem>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={`node:${n.id}`}>
                    {n.name}（{n.address}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {nodes.length === 0
                ? "还没有节点，先去「节点」页添加。"
                : "选中的机器会跑这个入站，订阅里会出现这台机器的入口。"}
            </p>
            {multiMachine && (
              <p className="text-xs text-muted-foreground">
                这个入站现在跑在多台机器上，保存后将只留选中的一台
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="inb-port">端口</Label>
            <Input
              id="inb-port"
              type="number"
              min={1}
              max={65535}
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              required
            />
          </div>

          {protocol === "vless-reality" && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-sni">SNI（伪装域名）</Label>
                  <Input
                    id="inb-sni"
                    value={sni}
                    onChange={(e) => setSni(e.target.value)}
                    placeholder="www.microsoft.com"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-flow">Flow</Label>
                  <Input
                    id="inb-flow"
                    value={flow}
                    onChange={(e) => setFlow(e.target.value)}
                  />
                </div>
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-priv">Reality 私钥</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => void genKeypair()}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    生成新密钥对
                  </Button>
                </div>
                <SecretInput
                  id="inb-priv"
                  value={privateKey}
                  onChange={setPrivateKey}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-pub">Reality 公钥</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => void copyPublicKey()}
                    disabled={!publicKey}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {copied ? (
                      <Check className="h-3.5 w-3.5" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                    复制
                  </Button>
                </div>
                <Input
                  id="inb-pub"
                  value={publicKey}
                  onChange={(e) => setPublicKey(e.target.value)}
                  placeholder="与私钥配对，生成密钥对时自动填入"
                  className="font-mono text-xs"
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-sid">Short ID</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setShortId(randomHex(4))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <Input
                  id="inb-sid"
                  value={shortId}
                  onChange={(e) => setShortId(e.target.value)}
                  placeholder="8 位十六进制，可留空自动生成"
                  className="font-mono text-xs"
                />
              </div>
            </>
          )}
          {protocol === "hysteria2" && (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-up">上行 Mbps（0 = 不限）</Label>
                  <Input
                    id="inb-up"
                    type="number"
                    min={0}
                    value={upMbps}
                    onChange={(e) => setUpMbps(Number(e.target.value))}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-down">下行 Mbps（0 = 不限）</Label>
                  <Input
                    id="inb-down"
                    type="number"
                    min={0}
                    value={downMbps}
                    onChange={(e) => setDownMbps(Number(e.target.value))}
                  />
                </div>
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-pw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-pw"
                  value={password}
                  onChange={setPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-obfs">混淆</Label>
                  <Select
                    value={obfs === "" ? "none" : obfs}
                    onValueChange={(v) => setObfs(v === "none" ? "" : v)}
                  >
                    <SelectTrigger id="inb-obfs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">无</SelectItem>
                      <SelectItem value="salamander">salamander</SelectItem>
                      {/* Temporary option for a value outside the fixed
                      list: shown while current, gone once a standard
                      option is picked. */}
                      {obfsCustom && (
                        <SelectItem value={obfsCustom.value}>
                          {obfsCustom.label}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                </div>
                {obfs === "salamander" && (
                  <div className="space-y-2">
                    <Label htmlFor="inb-obfspw">混淆密码</Label>
                    <SecretInput
                      id="inb-obfspw"
                      value={obfsPassword}
                      onChange={setObfsPassword}
                      placeholder="留空则自动生成"
                    />
                  </div>
                )}
              </div>
            </>
          )}
          {protocol === "tuic" && (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tuuid">UUID</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTuicUuid(crypto.randomUUID())}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <Input
                  id="inb-tuuid"
                  value={tuicUuid}
                  onChange={(e) => setTuicUuid(e.target.value)}
                  placeholder="留空则创建时自动生成"
                  className="font-mono text-xs"
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTuicPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-tpw"
                  value={tuicPassword}
                  onChange={setTuicPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-tcc">拥塞控制</Label>
                  <Select
                    value={tuicCongestion}
                    onValueChange={setTuicCongestion}
                  >
                    <SelectTrigger id="inb-tcc">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="bbr">BBR（推荐）</SelectItem>
                      <SelectItem value="cubic">Cubic</SelectItem>
                      <SelectItem value="new_reno">New Reno</SelectItem>
                      {/* Temporary option, same as obfs above. */}
                      {tuicCongestionCustom && (
                        <SelectItem value={tuicCongestionCustom.value}>
                          {tuicCongestionCustom.label}
                        </SelectItem>
                      )}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-tsni">SNI（可选）</Label>
                  <Input
                    id="inb-tsni"
                    value={tuicSni}
                    onChange={(e) => setTuicSni(e.target.value)}
                    placeholder="证书对应的域名，可留空"
                  />
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
          )}
          {protocol === "shadowsocks" && (
            <>
              <div className="space-y-2">
                <Label htmlFor="inb-ssm">加密方法</Label>
                <Select value={ssMethod} onValueChange={setSsMethod}>
                  <SelectTrigger id="inb-ssm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectLabel>2022 系列（推荐）</SelectLabel>
                      <SelectItem value="2022-blake3-aes-128-gcm">
                        2022-blake3-aes-128-gcm
                      </SelectItem>
                      <SelectItem value="2022-blake3-aes-256-gcm">
                        2022-blake3-aes-256-gcm
                      </SelectItem>
                      <SelectItem value="2022-blake3-chacha20-poly1305">
                        2022-blake3-chacha20-poly1305
                      </SelectItem>
                    </SelectGroup>
                    <SelectGroup>
                      <SelectLabel>经典系列</SelectLabel>
                      <SelectItem value="aes-128-gcm">aes-128-gcm</SelectItem>
                      <SelectItem value="aes-256-gcm">aes-256-gcm</SelectItem>
                      <SelectItem value="chacha20-ietf-poly1305">
                        chacha20-ietf-poly1305
                      </SelectItem>
                    </SelectGroup>
                    {/* Temporary option for a method outside the fixed
                    lists, same as obfs/tuic above. */}
                    {ssMethodCustom && (
                      <SelectItem value={ssMethodCustom.value}>
                        {ssMethodCustom.label}
                      </SelectItem>
                    )}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-sspw">
                    {SS2022_KEY_LEN[ssMethod] ? "密钥" : "密码"}
                  </Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() =>
                      setSsPassword(
                        SS2022_KEY_LEN[ssMethod]
                          ? randomBase64(SS2022_KEY_LEN[ssMethod])
                          : randomHex(8),
                      )
                    }
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-sspw"
                  value={ssPassword}
                  onChange={setSsPassword}
                  placeholder="留空则创建时自动生成"
                />
                {SS2022_KEY_LEN[ssMethod] && (
                  <p className="text-xs text-muted-foreground">
                    2022 系列的密钥是定长（{SS2022_KEY_LEN[ssMethod]}{" "}
                    字节）的 base64 值，长度不对会被拒绝。
                  </p>
                )}
              </div>
            </>
          )}
          {protocol === "anytls" && (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-alpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setAlPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-alpw"
                  value={alPassword}
                  onChange={setAlPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-alsni">SNI（可选）</Label>
                <Input
                  id="inb-alsni"
                  value={alSni}
                  onChange={(e) => setAlSni(e.target.value)}
                  placeholder="证书对应的域名，可留空"
                />
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
          )}
          {protocol === "trojan" && (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tjpw">访问密码</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTjPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    随机生成
                  </Button>
                </div>
                <SecretInput
                  id="inb-tjpw"
                  value={tjPassword}
                  onChange={setTjPassword}
                  placeholder="留空则创建时自动生成"
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-tjsni">SNI（可选）</Label>
                <Input
                  id="inb-tjsni"
                  value={tjSni}
                  onChange={(e) => setTjSni(e.target.value)}
                  placeholder="证书对应的域名，可留空"
                />
              </div>
              <p className="text-xs text-muted-foreground">
                服务器证书将使用「证书管理」中的当前证书（同步阶段生效）。
              </p>
            </>
          )}

          {formError && (
            <div className="rounded-[18px] border border-red-500/50 bg-red-500/10 px-4 py-3">
              <p className="text-sm font-medium text-red-600 dark:text-red-400">
                {formError}
              </p>
            </div>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={submitting}
            >
              取消
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? "保存中…" : creating ? "创建" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
      {/* Error popup: the inline banner above stays, but a modal
          on top is what actually gets noticed — the old faint red
          line at the bottom of the form was easy to miss. */}
      <Dialog
        open={formError !== null}
        onOpenChange={(open) => {
          if (!open) setFormError(null);
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>保存失败</DialogTitle>
            <DialogDescription>{formError ?? ""}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" onClick={() => setFormError(null)}>
              知道了
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
