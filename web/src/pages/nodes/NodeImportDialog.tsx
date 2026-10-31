import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  api,
  ApiError,
  type ImportCandidate,
  type NodeView,
} from "@/lib/api";

export function NodeImportDialog({
  node,
  candidates,
  onClose,
  onImported,
}: {
  node: NodeView;
  candidates: ImportCandidate[];
  onClose: () => void;
  onImported: () => Promise<void>;
}) {
  const importable = candidates.filter((c) => !c.disabledReason);
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(importable.map((c) => c.name)),
  );
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const handleConfirm = async () => {
    setSubmitting(true);
    setFormError(null);
    try {
      const res = await api.nodes.importInbounds(node.id, [...selected]);
      if (res.created.length > 0) {
        toast.success(
          `已导入 ${res.created.length} 个入站：${res.created.map((c) => c.name).join("、")}`,
        );
      }
      for (const s of res.skipped) {
        toast.warning(`${s.name} 未导入：${s.reason}`);
      }
      if (res.created.length === 0 && res.skipped.length === 0) {
        toast("没有导入任何入站");
      }
      await onImported();
    } catch (err) {
      setFormError(
        err instanceof ApiError ? err.message : "导入失败，请重试",
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
          <DialogTitle>从 {node.name} 导入入站</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">
          导入的入站不带用户，需加入订阅模板后才会有用户。导入后只跑在这个节点上。
        </p>
        <div className="space-y-2">
          {candidates.map((c) => (
            <label key={c.name} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={selected.has(c.name)}
                disabled={!!c.disabledReason}
                onChange={() => toggle(c.name)}
              />
              <span>{c.name}</span>
              <span className="text-muted-foreground">
                {c.protocol} · {c.port}
              </span>
              {c.disabledReason && (
                <span className="text-[13px] text-destructive">
                  {c.disabledReason}
                </span>
              )}
            </label>
          ))}
        </div>
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
          <Button
            onClick={handleConfirm}
            disabled={submitting || selected.size === 0}
          >
            {submitting ? "导入中…" : "导入"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
