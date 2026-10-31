import { useRef } from "react";
import { ClipboardPaste, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import i18n from "@/lib/i18n";

const TEXTAREA_CLS =
  "w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50";

// One PEM input with paste-from-clipboard and file-upload shortcuts.
export function PemField({
  id,
  label,
  placeholder,
  value,
  onChange,
  onError,
}: {
  id: string;
  label: string;
  placeholder: string;
  value: string;
  onChange: (v: string) => void;
  onError: (msg: string) => void;
}) {
  const fileRef = useRef<HTMLInputElement>(null);

  async function handlePaste() {
    try {
      const text = await navigator.clipboard.readText();
      if (text) onChange(text);
    } catch {
      onError(i18n.t('certificates.k042'));
    }
  }

  async function handleFile(file: File | undefined) {
    if (!file) return;
    try {
      onChange(await file.text());
    } catch {
      onError(i18n.t('certificates.k043'));
    }
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label htmlFor={id}>{label}</Label>
        <div className="flex gap-1.5">
          <Button
            type="button"
            variant="outline"
            onClick={handlePaste}
            className="h-7 px-2 text-xs"
          >
            <ClipboardPaste />
            {i18n.t('certificates.k062')}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => fileRef.current?.click()}
            className="h-7 px-2 text-xs"
          >
            <Upload />
            {i18n.t('certificates.k063')}
          </Button>
        </div>
      </div>
      <textarea
        id={id}
        rows={6}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className={TEXTAREA_CLS}
        spellCheck={false}
      />
      <input
        ref={fileRef}
        type="file"
        accept=".pem,.crt,.cer,.key,text/plain"
        className="hidden"
        onChange={(e) => {
          void handleFile(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
    </div>
  );
}

