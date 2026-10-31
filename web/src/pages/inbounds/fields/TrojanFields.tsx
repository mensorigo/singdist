import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";
import i18n from "@/lib/i18n";

interface TrojanFieldsProps {
  tjPassword: string;
  setTjPassword: (v: string) => void;
  tjSni: string;
  setTjSni: (v: string) => void;
}

export function TrojanFields({
  tjPassword,
  setTjPassword,
  tjSni,
  setTjSni,
}: TrojanFieldsProps) {
  return (
            <>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tjpw">{i18n.t('inbounds.fields.k019')}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTjPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {i18n.t('inbounds.fields.k035')}
                  </Button>
                </div>
                <SecretInput
                  id="inb-tjpw"
                  value={tjPassword}
                  onChange={setTjPassword}
                  placeholder={i18n.t('inbounds.fields.k020')}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="inb-tjsni">{i18n.t('inbounds.fields.k021')}</Label>
                <Input
                  id="inb-tjsni"
                  value={tjSni}
                  onChange={(e) => setTjSni(e.target.value)}
                  placeholder={i18n.t('inbounds.fields.k022')}
                />
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('inbounds.fields.k036')}
              </p>
            </>
  );
}
