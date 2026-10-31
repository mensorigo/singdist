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

interface Hy2FieldsProps {
  upMbps: number;
  setUpMbps: (v: number) => void;
  downMbps: number;
  setDownMbps: (v: number) => void;
  password: string;
  setPassword: (v: string) => void;
  obfs: string;
  setObfs: (v: string) => void;
  obfsPassword: string;
  setObfsPassword: (v: string) => void;
  obfsCustom: { value: string; label: string } | null;
}

export function Hy2Fields({
  upMbps,
  setUpMbps,
  downMbps,
  setDownMbps,
  password,
  setPassword,
  obfs,
  setObfs,
  obfsPassword,
  setObfsPassword,
  obfsCustom,
}: Hy2FieldsProps) {
  return (
            <>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-up">{i18n.t('inbounds.fields.k005')}</Label>
                  <Input
                    id="inb-up"
                    type="number"
                    min={0}
                    value={upMbps}
                    onChange={(e) => setUpMbps(Number(e.target.value))}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="inb-down">{i18n.t('inbounds.fields.k006')}</Label>
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
                  <Label htmlFor="inb-pw">{i18n.t('inbounds.fields.k007')}</Label>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setPassword(randomHex(8))}
                    className="h-7 rounded-lg px-2 text-xs"
                  >
                    {i18n.t('inbounds.fields.k031')}
                  </Button>
                </div>
                <SecretInput
                  id="inb-pw"
                  value={password}
                  onChange={setPassword}
                  placeholder={i18n.t('inbounds.fields.k008')}
                />
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="inb-obfs">{i18n.t('inbounds.fields.k009')}</Label>
                  <Select
                    value={obfs === "" ? "none" : obfs}
                    onValueChange={(v) => setObfs(v === "none" ? "" : v)}
                  >
                    <SelectTrigger id="inb-obfs">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">{i18n.t('inbounds.fields.k010')}</SelectItem>
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
                    <Label htmlFor="inb-obfspw">{i18n.t('inbounds.fields.k011')}</Label>
                    <SecretInput
                      id="inb-obfspw"
                      value={obfsPassword}
                      onChange={setObfsPassword}
                      placeholder={i18n.t('inbounds.fields.k012')}
                    />
                  </div>
                )}
              </div>
            </>
  );
}
