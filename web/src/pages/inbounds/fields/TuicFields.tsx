import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SecretInput } from "../SecretInput";
import { randomHex } from "../helpers";
import i18n from "@/lib/i18n";

interface TuicFieldsProps {
  tuicUuid: string;
  setTuicUuid: (v: string) => void;
  tuicPassword: string;
  setTuicPassword: (v: string) => void;
  tuicCongestion: string;
  setTuicCongestion: (v: string) => void;
  tuicCongestionCustom: { value: string; label: string } | null;
  tuicSni: string;
  setTuicSni: (v: string) => void;
}

export function TuicFields({
  tuicUuid,
  setTuicUuid,
  tuicPassword,
  setTuicPassword,
  tuicCongestion,
  setTuicCongestion,
  tuicCongestionCustom,
  tuicSni,
  setTuicSni,
}: TuicFieldsProps) {
  return (
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
                    {i18n.t('inbounds.fields.k037')}
                  </Button>
                </div>
                <Input
                  id="inb-tuuid"
                  value={tuicUuid}
                  onChange={(e) => setTuicUuid(e.target.value)}
                  placeholder={i18n.t('inbounds.fields.k023')}
                  className="font-mono text-xs"
                />
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="inb-tpw">{i18n.t('inbounds.fields.k024')}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setTuicPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {i18n.t('inbounds.fields.k038')}
                  </Button>
                </div>
                <SecretInput
                  id="inb-tpw"
                  value={tuicPassword}
                  onChange={setTuicPassword}
                  placeholder={i18n.t('inbounds.fields.k023')}
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-tcc">{i18n.t('inbounds.fields.k025')}</Label>
                  <Select
                    value={tuicCongestion}
                    onValueChange={setTuicCongestion}
                  >
                    <SelectTrigger id="inb-tcc">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="bbr">{i18n.t('inbounds.fields.k026')}</SelectItem>
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
                  <Label htmlFor="inb-tsni">{i18n.t('inbounds.fields.k027')}</Label>
                  <Input
                    id="inb-tsni"
                    value={tuicSni}
                    onChange={(e) => setTuicSni(e.target.value)}
                    placeholder={i18n.t('inbounds.fields.k028')}
                  />
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                {i18n.t('inbounds.fields.k039')}
              </p>
            </>
  );
}
