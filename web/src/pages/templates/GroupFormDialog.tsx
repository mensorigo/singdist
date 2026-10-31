import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { InboundView } from "@/lib/api";
import { GROUP_TYPE_LABEL, isDefaultGroup, type GroupDraft, type GroupType } from "./group";

const PROTOCOL_LABEL: Record<string, string> = {
  "vless-reality": "VLESS · Reality",
  hysteria2: "Hysteria2",
  tuic: "TUIC v5",
  anytls: "AnyTLS",
  trojan: "Trojan",
};

export function GroupFormDialog({
  item,
  inbounds,
  onClose,
  onSaved,
}: {
  item: GroupDraft | null;
  inbounds: InboundView[];
  onClose: () => void;
  onSaved: (g: GroupDraft) => void;
}) {
  const creating = item === null;
  const [gName, setGName] = useState(item?.name ?? "");
  const [gType, setGType] = useState<GroupType>(item?.type ?? "select");
  const [gNodes, setGNodes] = useState<number[]>(item?.nodeIds ?? []);
  const [gUrl, setGUrl] = useState(
    item?.url ?? "https://www.gstatic.com/generate_204",
  );
  const [gInterval, setGInterval] = useState(String(item?.interval ?? 300));
  const [gTolerance, setGTolerance] = useState(String(item?.tolerance ?? 50));
  const defaultGroup = item !== null && isDefaultGroup(item);
  const [gError, setGError] = useState<string | null>(null);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (gName.trim() === "") {
      setGError("请填分组名字");
      return;
    }
    onSaved({
      id: item?.id ?? `local-${Date.now()}`,
      name: gName.trim(),
      type: gType,
      nodeIds: gNodes,
      enabled: item?.enabled ?? true,
      url: gUrl,
      interval: Number(gInterval) || 300,
      tolerance: Number(gTolerance) || 50,
    });
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-h-[85vh] max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{creating ? "新建分组" : "编辑分组"}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="grp-name">
              分组名字{defaultGroup ? "（系统默认，不可改）" : ""}
            </Label>
            <Input
              id="grp-name"
              value={gName}
              onChange={(e) => setGName(e.target.value)}
              placeholder="例如：香港组"
              disabled={defaultGroup}
            />
          </div>
          <div className="space-y-2">
            <Label>类型</Label>
            <Select
              value={gType}
              onValueChange={(v) => setGType(v as GroupType)}
              disabled={defaultGroup}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(GROUP_TYPE_LABEL) as GroupType[]).map((t) => (
                  <SelectItem key={t} value={t}>
                    {GROUP_TYPE_LABEL[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>包含的节点</Label>
              <label className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={
                    inbounds.length > 0 &&
                    inbounds.every((ib) => gNodes.includes(ib.id))
                  }
                  onChange={(e) =>
                    setGNodes(
                      e.target.checked ? inbounds.map((ib) => ib.id) : [],
                    )
                  }
                  className="h-4 w-4 accent-primary"
                />
                全选
              </label>
            </div>
            <div className="max-h-[220px] space-y-1.5 overflow-y-auto pr-1">
              {inbounds.map((ib) => (
                <label
                  key={ib.id}
                  className="flex cursor-pointer items-center gap-3 rounded-lg border px-3 py-2 text-sm transition-colors hover:bg-muted/50"
                >
                  <input
                    type="checkbox"
                    checked={gNodes.includes(ib.id)}
                    onChange={() =>
                      setGNodes((prev) =>
                        prev.includes(ib.id)
                          ? prev.filter((x) => x !== ib.id)
                          : [...prev, ib.id],
                      )
                    }
                    className="h-4 w-4 accent-primary"
                  />
                  <span className="font-medium">{ib.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {PROTOCOL_LABEL[ib.protocol] ?? ib.protocol} · 端口{" "}
                    {ib.port}
                    {!ib.enabled && " · 已停用"}
                  </span>
                </label>
              ))}
              {inbounds.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  暂无入站，请先在「入站」视图创建
                </p>
              )}
            </div>
          </div>
          <details className="space-y-2">
            <summary className="cursor-pointer text-sm text-muted-foreground">
              高级（测速地址、间隔、容差）
            </summary>
            <div className="space-y-2 pt-1">
              <div className="space-y-2">
                <Label htmlFor="grp-url">测速地址</Label>
                <Input
                  id="grp-url"
                  value={gUrl}
                  onChange={(e) => setGUrl(e.target.value)}
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-2">
                  <Label htmlFor="grp-interval">间隔（秒）</Label>
                  <Input
                    id="grp-interval"
                    type="number"
                    value={gInterval}
                    onChange={(e) => setGInterval(e.target.value)}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="grp-tolerance">容差（毫秒）</Label>
                  <Input
                    id="grp-tolerance"
                    type="number"
                    value={gTolerance}
                    onChange={(e) => setGTolerance(e.target.value)}
                  />
                </div>
              </div>
            </div>
          </details>
          {gError && (
            <p className="text-[13px] text-destructive">{gError}</p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              取消
            </Button>
            <Button type="submit">{creating ? "添加" : "保存"}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
