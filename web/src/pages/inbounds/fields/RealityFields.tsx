import { Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";
import i18n from "@/lib/i18n";

interface RealityFieldsProps {
  sni: string;
  setSni: (v: string) => void;
  flow: string;
  setFlow: (v: string) => void;
  publicKey: string;
  setPublicKey: (v: string) => void;
  privateKey: string;
  setPrivateKey: (v: string) => void;
  shortId: string;
  setShortId: (v: string) => void;
  copied: boolean;
  copyPublicKey: () => Promise<void>;
  genKeypair: () => Promise<void>;
}

export function RealityFields({
  sni,
  setSni,
  flow,
  setFlow,
  publicKey,
  setPublicKey,
  privateKey,
  setPrivateKey,
  shortId,
  setShortId,
  copied,
  copyPublicKey,
  genKeypair,
}: RealityFieldsProps) {
  return (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-sni">{i18n.t('inbounds.fields.k013')}</Label>
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
                  <Label htmlFor="inb-priv">{i18n.t('inbounds.fields.k014')}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => void genKeypair()}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {i18n.t('inbounds.fields.k032')}
                  </Button>
                </div>
                <SecretInput
                  id="inb-priv"
                  value={privateKey}
                  onChange={setPrivateKey}
                  placeholder={i18n.t('inbounds.fields.k015')}
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-pub">{i18n.t('inbounds.fields.k016')}</Label>
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
                    {i18n.t('inbounds.fields.k033')}
                  </Button>
                </div>
                <Input
                  id="inb-pub"
                  value={publicKey}
                  onChange={(e) => setPublicKey(e.target.value)}
                  placeholder={i18n.t('inbounds.fields.k017')}
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
                    {i18n.t('inbounds.fields.k034')}
                  </Button>
                </div>
                <Input
                  id="inb-sid"
                  value={shortId}
                  onChange={(e) => setShortId(e.target.value)}
                  placeholder={i18n.t('inbounds.fields.k018')}
                  className="font-mono text-xs"
                />
              </div>
            </>
  );
}
