import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  api,
  ApiError,
  type AnyTLSParams,
  type Hy2Params,
  type InboundNodeAssignment,
  type InboundProtocol,
  type InboundView,
  type NodeView,
  type RealityParams,
  type TrojanParams,
  type TuicParams,
} from "@/lib/api";
import { customSelectOption } from "@/lib/custom-option";
import {
  DEFAULT_PORT,
  OBFS_OPTIONS,
  PROTOCOL_LABEL,
  TUIC_CONGESTION_OPTIONS,
} from "./constants";
import { formatLocalLabel } from "./helpers";
import { DiffsList } from "./DiffsList";
import { RealityFields } from "./fields/RealityFields";
import { Hy2Fields } from "./fields/Hy2Fields";
import { TuicFields } from "./fields/TuicFields";
import { AnyTlsFields } from "./fields/AnyTlsFields";
import { TrojanFields } from "./fields/TrojanFields";
import { buildParams } from "./params";
import i18n from "@/lib/i18n";

export function InboundFormDialog({
  item,
  onClose,
  onSaved,
  showWarnings,
}: {
  item: InboundView | null;
  onClose: () => void;
  onSaved: () => Promise<void>;
  showWarnings: boolean;
}) {
  const creating = item === null;
  const [protocol, setProtocol] = useState<InboundProtocol>(
    item?.protocol ?? "vless-reality",
  );
  const [name, setName] = useState(item?.name ?? "");
  const [port, setPort] = useState(item?.port ?? 443);

  const rp = item?.protocol === "vless-reality" ? (item.params as RealityParams) : null;
  const [sni, setSni] = useState(rp?.sni ?? "");
  const [flow, setFlow] = useState(rp?.flow ?? "xtls-rprx-vision");
  const [publicKey, setPublicKey] = useState(rp?.publicKey ?? "");
  const [privateKey, setPrivateKey] = useState(rp?.privateKey ?? "");
  const [shortId, setShortId] = useState(rp?.shortId ?? "");

  const hp = item?.protocol === "hysteria2" ? (item.params as Hy2Params) : null;
  const [upMbps, setUpMbps] = useState(hp?.upMbps ?? 0);
  const [downMbps, setDownMbps] = useState(hp?.downMbps ?? 0);
  const [password, setPassword] = useState(hp?.password ?? "");
  const [obfs, setObfs] = useState(hp?.obfs ?? "");
  const [obfsPassword, setObfsPassword] = useState(hp?.obfsPassword ?? "");

  const tp = item?.protocol === "tuic" ? (item.params as TuicParams) : null;
  const [tuicUuid, setTuicUuid] = useState(tp?.uuid ?? "");
  const [tuicPassword, setTuicPassword] = useState(tp?.password ?? "");
  const [tuicCongestion, setTuicCongestion] = useState(
    tp?.congestionControl ?? "bbr",
  );
  const [tuicSni, setTuicSni] = useState(tp?.sni ?? "");


  // Temporary dropdown options for values outside the fixed lists (e.g.
  // imported from a config file): shown while current, gone once the
  // user picks a standard option.
  const obfsCustom = customSelectOption(
    OBFS_OPTIONS,
    obfs === "" ? "none" : obfs,
  );
  const tuicCongestionCustom = customSelectOption(
    TUIC_CONGESTION_OPTIONS,
    tuicCongestion,
  );

  const ap = item?.protocol === "anytls" ? (item.params as AnyTLSParams) : null;
  const [alPassword, setAlPassword] = useState(ap?.password ?? "");
  const [alSni, setAlSni] = useState(ap?.sni ?? "");

  const jp = item?.protocol === "trojan" ? (item.params as TrojanParams) : null;
  const [tjPassword, setTjPassword] = useState(jp?.password ?? "");
  const [tjSni, setTjSni] = useState(jp?.sni ?? "");

  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // One inbound runs on exactly ONE machine: 本机 (local) or one node.
  // `machine` is "local", "node:<id>", or "" while the stored value is
  // still being resolved (edit only; "" preserves stored assignments).
  const [nodes, setNodes] = useState<NodeView[]>([]);
  // 本机 row label: hostname + (IP:port) once loaded, plain 本机 until
  // then (and forever if the endpoint fails — the form never breaks).
  const [localLabel, setLocalLabel] = useState(i18n.t('inbounds.k015'));
  const [machine, setMachine] = useState<string>(
    creating ? "local" : item?.runLocal ? "local" : "",
  );
  // Stored assignments (for the multi-machine note + changed detection).
  const [initialAssignments, setInitialAssignments] = useState<
    InboundNodeAssignment[]
  >([]);
  const [assignmentsLoaded, setAssignmentsLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      // Best effort: a failed fetch keeps the plain 本机 label.
      try {
        const lm = await api.localMachine();
        if (!cancelled) setLocalLabel(formatLocalLabel(lm));
      } catch {
        // Keep the fallback label; the form must never break on this.
      }
      try {
        const list = await api.nodes.list();
        if (cancelled) return;
        setNodes(list);
        if (!creating && item) {
          const res = await api.nodes.getInboundNodes(item.id);
          if (cancelled) return;
          const assigns = res.nodes ?? [];
          setInitialAssignments(assigns);
          setAssignmentsLoaded(true);
          if (item.runLocal) {
            setMachine("local");
          } else if (assigns.length > 0) {
            const first = assigns[0].nodeId;
            // Fallback to 本机 when the stored node is gone (e.g. deleted).
            setMachine(
              list.some((n) => n.id === first) ? `node:${first}` : "local",
            );
          } else {
            // No machine stored at all → default to 本机.
            setMachine("local");
          }
        }
      } catch {
        if (!cancelled) setNodes([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [creating, item]);

  const copyPublicKey = async () => {
    try {
      await navigator.clipboard.writeText(publicKey);
      setCopied(true);
      toast.success(i18n.t('inbounds.k016'));
      setTimeout(() => setCopied(false), 1500);
    } catch {
      setFormError(i18n.t('inbounds.k017'));
    }
  };

  const genKeypair = async () => {
    try {
      const kp = await api.inbounds.keypair();
      setPublicKey(kp.publicKey);
      setPrivateKey(kp.privateKey);
      if (!shortId) setShortId(kp.shortId);
    } catch {
      setFormError(i18n.t('inbounds.k018'));
    }
  };


  // Edit only: is the stored inbound on multiple machines? (run_local +
  // assignments, or several assignments.) Saving collapses it to the one
  // machine picked in the dropdown.
  let multiMachine = false;
  if (!creating && item && assignmentsLoaded) {
    multiMachine =
      (item.runLocal && initialAssignments.length > 0) ||
      initialAssignments.length > 1;
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const params = buildParams({ protocol, sni, flow, publicKey, privateKey, shortId, upMbps, downMbps, password, obfs, obfsPassword, tuicUuid, tuicPassword, tuicCongestion, tuicSni, alPassword, alSni, tjPassword, tjSni });
      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        setFormError(i18n.t('inbounds.k019'));
        return;
      }
      // "" means the stored machine never resolved (fetch failed): keep
      // the stored runLocal and touch no assignments.
      const runLocal =
        machine === "" ? (item?.runLocal ?? true) : machine === "local";
      const nodeId = machine.startsWith("node:")
        ? Number(machine.slice(5))
        : null;
      // The single port box is the port on the one selected machine.
      const assignments: InboundNodeAssignment[] =
        nodeId !== null ? [{ nodeId, port }] : [];
      let inboundId: number;
      if (creating) {
        const created = await api.inbounds.create({
          name: name.trim(),
          protocol,
          port,
          params,
          runLocal,
        });
        inboundId = created.id;
        toast.success(i18n.t('inbounds.k020'));
      } else {
        await api.inbounds.update(item.id, {
          name: name.trim(),
          port,
          params,
          runLocal,
        });
        inboundId = item.id;
        toast.success(i18n.t('inbounds.k021'));
      }
      // Create: save assignments only when a node is picked (local needs none).
      // Edit: save only when the assignment set actually changed, so an
      // unrelated edit never fails on an unreachable node. Stored port 0
      // means "the inbound's port", so it normalizes to item.port.
      // Skipped entirely when assignments never loaded (preserves them).
      if (creating) {
        if (assignments.length > 0) {
          await api.nodes.setInboundNodes(inboundId, assignments);
        }
      } else if (assignmentsLoaded) {
        const normalize = (list: InboundNodeAssignment[]) =>
          new Map(
            list.map((a) => [a.nodeId, a.port > 0 ? a.port : item.port]),
          );
        const oldMap = normalize(initialAssignments);
        const newMap = normalize(assignments);
        const changed =
          oldMap.size !== newMap.size ||
          [...newMap].some(([id, p]) => oldMap.get(id) !== p);
        if (changed) {
          await api.nodes.setInboundNodes(inboundId, assignments);
        }
      }
      await onSaved();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : i18n.t('inbounds.k022'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{creating ? i18n.t('inbounds.k023') : i18n.t('inbounds.k024')}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          {showWarnings && !creating && item.status === "drifted" && item.diffs && item.diffs.length > 0 && (
            <div className="rounded-[18px] border border-amber-500/40 bg-amber-500/10 px-4 py-3">
              <p className="text-[13px] font-medium text-amber-700 dark:text-amber-300">
                {i18n.t('inbounds.k065')}
              </p>
              <DiffsList diffs={item.diffs} />
            </div>
          )}
          {creating && (
            <div className="space-y-2">
              <Label>{i18n.t('inbounds.k025')}</Label>
              <div className="grid grid-cols-3 gap-2">
                {(
                  Object.keys(PROTOCOL_LABEL) as InboundProtocol[]
                ).map((key) => (
                  <Button
                    key={key}
                    type="button"
                    variant={protocol === key ? "default" : "outline"}
                    onClick={() => {
                      setProtocol(key);
                      setPort(DEFAULT_PORT[key]);
                    }}
                    className="h-8 rounded-xl px-2 text-[13px]"
                  >
                    {PROTOCOL_LABEL[key]}
                  </Button>
                ))}
              </div>
            </div>
          )}
          <div className="space-y-2">
            <Label htmlFor="inb-name">{i18n.t('inbounds.k026')}</Label>
            <Input
              id="inb-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={i18n.t('inbounds.k027')}
              required
            />
          </div>

          <div className="space-y-2">
            <Label>{i18n.t('inbounds.k028')}</Label>
            <Select value={machine} onValueChange={setMachine}>
              <SelectTrigger>
                <SelectValue placeholder={i18n.t('inbounds.k029')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="local">{localLabel}</SelectItem>
                {nodes.map((n) => (
                  <SelectItem key={n.id} value={`node:${n.id}`}>
                    {n.name}（{n.address}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {nodes.length === 0
                ? i18n.t('inbounds.k030')
                : i18n.t('inbounds.k031')}
            </p>
            {multiMachine && (
              <p className="text-xs text-muted-foreground">
                {i18n.t('inbounds.k066')}
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="inb-port">{i18n.t('inbounds.k032')}</Label>
            <Input
              id="inb-port"
              type="number"
              min={1}
              max={65535}
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              required
            />
          </div>

          {protocol === "vless-reality" && (
            <RealityFields
              sni={sni}
              setSni={setSni}
              flow={flow}
              setFlow={setFlow}
              publicKey={publicKey}
              setPublicKey={setPublicKey}
              privateKey={privateKey}
              setPrivateKey={setPrivateKey}
              shortId={shortId}
              setShortId={setShortId}
              copied={copied}
              copyPublicKey={copyPublicKey}
              genKeypair={genKeypair}
            />
          )}
          {protocol === "hysteria2" && (
            <Hy2Fields
              upMbps={upMbps}
              setUpMbps={setUpMbps}
              downMbps={downMbps}
              setDownMbps={setDownMbps}
              password={password}
              setPassword={setPassword}
              obfs={obfs}
              setObfs={setObfs}
              obfsPassword={obfsPassword}
              setObfsPassword={setObfsPassword}
              obfsCustom={obfsCustom}
            />
          )}
          {protocol === "tuic" && (
            <TuicFields
              tuicUuid={tuicUuid}
              setTuicUuid={setTuicUuid}
              tuicPassword={tuicPassword}
              setTuicPassword={setTuicPassword}
              tuicCongestion={tuicCongestion}
              setTuicCongestion={setTuicCongestion}
              tuicCongestionCustom={tuicCongestionCustom}
              tuicSni={tuicSni}
              setTuicSni={setTuicSni}
            />
          )}
          {protocol === "anytls" && (
            <AnyTlsFields
              alPassword={alPassword}
              setAlPassword={setAlPassword}
              alSni={alSni}
              setAlSni={setAlSni}
            />
          )}
          {protocol === "trojan" && (
            <TrojanFields
              tjPassword={tjPassword}
              setTjPassword={setTjPassword}
              tjSni={tjSni}
              setTjSni={setTjSni}
            />
          )}

          {formError && (
            <div className="rounded-[18px] border border-red-500/50 bg-red-500/10 px-4 py-3">
              <p className="text-sm font-medium text-red-600 dark:text-red-400">
                {formError}
              </p>
            </div>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={onClose}
              disabled={submitting}
            >
              {i18n.t('inbounds.k067')}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? i18n.t('inbounds.k033') : creating ? i18n.t('inbounds.k034') : i18n.t('inbounds.k035')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
      {/* Error popup: the inline banner above stays, but a modal
          on top is what actually gets noticed — the old faint red
          line at the bottom of the form was easy to miss. */}
      <Dialog
        open={formError !== null}
        onOpenChange={(open) => {
          if (!open) setFormError(null);
        }}
      >
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{i18n.t('inbounds.k036')}</DialogTitle>
            <DialogDescription>{formError ?? ""}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" onClick={() => setFormError(null)}>
              {i18n.t('inbounds.k068')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
