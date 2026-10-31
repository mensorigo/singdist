import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  api,
  ApiError,
  type InboundView,
  type NodeView,
} from "@/lib/api";
import { normalizeServerIP } from "./format";

export function NodeFormDialog({
  item,
  onClose,
  onSaved,
  onCreated,
}: {
  item: NodeView | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onCreated?: (created: NodeView) => void;
}) {
  const creating = item === null;
  const [name, setName] = useState(item?.name ?? "");
  const [address, setAddress] = useState(item?.address ?? "");
  const [pairingCode, setPairingCode] = useState("");
  const [pairingLock, setPairingLock] = useState(0);
  const [pairingRequested, setPairingRequested] = useState(false);
  const [publicHost, setPublicHost] = useState(item?.publicHost ?? "");
  const [assignedNames, setAssignedNames] = useState<string[] | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    if (creating || !item) return;
    let cancelled = false;
    (async () => {
      try {
        const [detail, list] = await Promise.all([
          api.nodes.get(item.id),
          api.inbounds.list(),
        ]);
        if (cancelled) return;
        const byId = new Map<number, InboundView>();
        for (const ib of list.items) byId.set(ib.id, ib);
        setAssignedNames(
          detail.inboundIds.map((id) => byId.get(id)?.name ?? `#${id}`),
        );
      } catch {
        if (!cancelled) setAssignedNames([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [creating, item]);

  // Countdown for the pairing-request lock (agent enforces 60s;
  // the frontend mirrors it so the button cannot be spammed).
  useEffect(() => {
    if (pairingLock <= 0) return;
    const timer = setTimeout(() => setPairingLock((v) => Math.max(0, v - 1)), 1000);
    return () => clearTimeout(timer);
  }, [pairingLock]);

  const handleRequestPairingCode = async () => {
    const addr = normalizeServerIP(address);
    if (addr === "") {
      setFormError("请先填服务器IP");
      return;
    }
    setFormError(null);
    try {
      const res = await api.nodes.requestPairingCode(addr);
      setPairingRequested(true);
      setPairingLock(res.retry_after > 0 ? res.retry_after : 60);
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : "请求生成配对码失败",
      );
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      let created: NodeView | null = null;
      // The pairing code is the only credential in this form: the
      // panel exchanges it for a long-term token and stores that
      // itself, so no token is ever typed here. Creating requires a
      // code; editing with an empty code sends no credential field
      // at all, and the backend keeps the existing token.
      const code = pairingCode.trim();
      if (creating && code === "") {
        setFormError("请先填配对码");
        setSubmitting(false);
        return;
      }
      if (creating) {
        created = await api.nodes.create({
          name: name.trim(),
          address: normalizeServerIP(address),
          pairing_code: code,
          publicHost: publicHost.trim() || undefined,
        });
        toast.success("节点已添加（5 秒内开始首次推送和状态探测）");
        if (created.warning) {
          toast.warning(created.warning);
        }
      } else {
        await api.nodes.update(item.id, {
          name: name.trim(),
          address: normalizeServerIP(address),
          pairing_code: code || undefined,
          publicHost: publicHost.trim(),
        });
        toast.success("节点已保存");
      }
      await onSaved();
      // Creation-time probe succeeded (no warning): auto-scan the
      // node's core once for importable inbounds. The scan itself
      // stays silent unless something can actually be imported.
      if (creating && created && !created.warning) {
        onCreated?.(created);
      }
    } catch (err) {
      setFormError(
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "保存失败，请重试",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{creating ? "添加节点" : "编辑节点"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="node-name">备注（选填）</Label>
            <Input
              id="node-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="留空则自动使用主机名"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-address">服务器IP</Label>
            <Input
              id="node-address"
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              placeholder="IP地址"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-pairing">配对码</Label>
            <div className="flex gap-2">
              <Input
                id="node-pairing"
                value={pairingCode}
                onChange={(e) =>
                  setPairingCode(
                    e.target.value.replace(/[^A-Za-z0-9]/g, "").slice(0, 8),
                  )
                }
                placeholder={
                  creating
                    ? "8 位配对码，子服务器终端查看"
                    : "留空保持不变，填了则重新配对"
                }
                maxLength={8}
                className="flex-1"
              />
              <Button
                type="button"
                variant="outline"
                onClick={handleRequestPairingCode}
                disabled={pairingLock > 0 || submitting}
              >
                {pairingLock > 0 ? `发送请求（${pairingLock}s）` : "发送请求"}
              </Button>
            </div>
            {pairingRequested && (
              <p className="text-sm text-muted-foreground">
                已请求生成，请去子服务器终端查看配对码，5分钟内有效。
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="node-public">对外地址（可选）</Label>
            <Input
              id="node-public"
              value={publicHost}
              onChange={(e) => setPublicHost(e.target.value)}
              placeholder="节点对外地址显示，默认使用IP地址，可以使用域名"
            />
          </div>
          {!creating && assignedNames !== null && (
            <p className="text-sm text-muted-foreground">
              已分配入站：
              {assignedNames.length > 0 ? assignedNames.join("、") : "无（仅本地）"}
              。分配在入站编辑里改。
            </p>
          )}
          {formError && (
            <p className="text-[13px] text-destructive">{formError}</p>
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
              {submitting ? "保存中…" : creating ? "添加" : "保存"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
