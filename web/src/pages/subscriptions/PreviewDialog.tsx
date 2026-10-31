import { useEffect, useState } from "react";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { api, ApiError } from "@/lib/api";
import { copyText } from "./format";

export function PreviewDialog({
  subId,
  name,
  onClose,
}: {
  subId: number;
  name: string;
  onClose: () => void;
}) {
  const [content, setContent] = useState<string | null>(null);
  const [format, setFormat] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    api.subscriptions
      .preview(subId)
      .then((v) => {
        if (live) {
          setContent(v.content);
          setFormat(v.format);
        }
      })
      .catch((err) => {
        if (live) {
          setError(
            err instanceof ApiError ? err.message : "预览生成失败，请重试",
          );
        }
      });
    return () => {
      live = false;
    };
  }, [subId]);

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>
            订阅预览 · {name}
            {format && (
              <span className="ml-2 text-sm font-normal text-muted-foreground">
                {format === "clash" ? "Clash" : "sing-box JSON"}
              </span>
            )}
          </DialogTitle>
        </DialogHeader>
        {error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : content === null ? (
          <p className="text-sm text-muted-foreground">生成中…</p>
        ) : (
          <>
            <pre className="max-h-[55vh] overflow-auto rounded-lg border bg-muted/40 p-4 font-mono text-xs leading-relaxed">
              {content}
            </pre>
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => void copyText(content, "订阅内容已复制")}
              >
                <Copy className="h-4 w-4" />
                复制全文
              </Button>
              <Button type="button" onClick={onClose}>
                关闭
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
