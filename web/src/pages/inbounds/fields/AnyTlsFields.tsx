import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";
import i18n from "@/lib/i18n";

interface AnyTlsFieldsProps {
  alPassword: string;
  setAlPassword: (v: string) => void;
  alSni: string;
  setAlSni: (v: string) => void;
}

export function AnyTlsFields({
  alPassword,
  setAlPassword,
  alSni,
  setAlSni,
}: AnyTlsFieldsProps) {
  return (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-alpw">{i18n.t('inbounds.fields.k001')}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setAlPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {i18n.t('inbounds.fields.k029')}
                  </Button>
                </div>
                <SecretInput
                  id="inb-alpw"
                  value={alPassword}
                  onChange={setAlPassword}
                  placeholder={i18n.t('inbounds.fields.k002')}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-alsni">{i18n.t('inbounds.fields.k003')}</Label>
                <Input
                  id="inb-alsni"
                  value={alSni}
                  onChange={(e) => setAlSni(e.target.value)}
                  placeholder={i18n.t('inbounds.fields.k004')}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('inbounds.fields.k030')}
              </p>
            </>
  );
}
