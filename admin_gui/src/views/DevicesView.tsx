import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Cpu, Loader2, Wifi } from "lucide-react";
import { devicesService, type Device } from "@/services/devices";
import { type Org } from "@/services/orgs";
import { MSG } from "@/lib/constants";
import { MAC_RE, formatMAC } from "@/lib/utils";
import { useOrgSelector } from "@/hooks/useOrgSelector";
import { SelectOrgPrompt, EmptyState, QueryErrorState } from "@/components/TableStates";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@/components/ui/alert-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useToast } from "@/hooks/useToast";

export function DevicesView() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { orgs, selectedOrgId, setSelectedOrgId, isError: orgsError } = useOrgSelector();
  const [showAdd, setShowAdd] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Device | null>(null);

  const orgMap = Object.fromEntries((orgs as Org[]).map((o) => [o.id, o.name]));

  const { data: devices = [], isLoading, isError: devicesError } = useQuery({
    queryKey: ["devices", selectedOrgId],
    queryFn: () => devicesService.listByOrg(Number(selectedOrgId)),
    enabled: !!selectedOrgId,
  });

  const addMutation = useMutation({
    mutationFn: ({ orgId, macId }: { orgId: number; macId: string }) =>
      devicesService.add(orgId, macId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["devices"] });
      setShowAdd(false);
      toast({ title: MSG.Devices.REGISTERED, variant: "success" });
    },
    onError: (err: Error) => {
      toast({ title: MSG.Devices.REGISTER_FAILED, description: err.message, variant: "destructive" });
    },
  });

  const removeMutation = useMutation({
    mutationFn: ({ orgId, macId }: { orgId: number; macId: string }) =>
      devicesService.remove(orgId, macId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["devices"] });
      setDeleteTarget(null);
      toast({ title: MSG.Devices.REMOVED, variant: "success" });
    },
    onError: (err: Error) => {
      toast({ title: MSG.Devices.REMOVE_FAILED, description: err.message, variant: "destructive" });
    },
  });

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <Label className="whitespace-nowrap text-sm text-muted-foreground">Organization</Label>
          <Select value={selectedOrgId} onValueChange={setSelectedOrgId}>
            <SelectTrigger className="w-52">
              <SelectValue placeholder="Select org…" />
            </SelectTrigger>
            <SelectContent>
              {orgs.map((o) => (
                <SelectItem key={o.id} value={String(o.id)}>
                  {o.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <Button size="sm" disabled={!selectedOrgId} onClick={() => setShowAdd(true)}>
          <Plus className="h-4 w-4" />
          Register Device
        </Button>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {orgsError ? (
          <QueryErrorState message="Failed to load organizations. Please refresh the page." />
        ) : !selectedOrgId ? (
          <SelectOrgPrompt icon={Cpu} message={MSG.Devices.SELECT_ORG_PROMPT} />
        ) : isLoading ? (
          <div className="flex h-48 items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : devicesError ? (
          <QueryErrorState message="Failed to load devices. Please refresh the page." />
        ) : devices.length === 0 ? (
          <EmptyState
            icon={Cpu}
            title={MSG.Devices.EMPTY_TITLE}
            body={MSG.Devices.EMPTY_BODY}
            addLabel="Register Device"
            onAdd={() => setShowAdd(true)}
          />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40">
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">MAC Address</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Organization</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Status</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Registered</th>
                <th className="px-4 py-3 text-right font-medium text-muted-foreground">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {devices.map((d) => (
                <tr key={d.id} className="hover:bg-muted/20 transition-colors">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <Cpu className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                      <span className="font-mono text-xs tracking-wide">
                        {formatMAC(d.id)}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground text-xs">
                    {orgMap[d.org_id] ?? "Unknown"}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant="success">
                      <Wifi className="h-3 w-3 mr-1" />
                      Active
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {new Date(d.added_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      onClick={() => setDeleteTarget(d)}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Add Device Dialog */}
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Register Device</DialogTitle>
            <DialogDescription>
              Enter the MAC address of the device to register it to the selected organization.
            </DialogDescription>
          </DialogHeader>
          <AddDeviceForm
            isPending={addMutation.isPending}
            onSubmit={(macId) =>
              addMutation.mutate({ orgId: Number(selectedOrgId), macId })
            }
            onCancel={() => setShowAdd(false)}
          />
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove device?</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget ? MSG.Devices.REMOVE_BODY(formatMAC(deleteTarget.id)) : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                deleteTarget &&
                removeMutation.mutate({
                  orgId: Number(selectedOrgId),
                  macId: deleteTarget.id,
                })
              }
              disabled={removeMutation.isPending}
            >
              {removeMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function AddDeviceForm({
  isPending,
  onSubmit,
  onCancel,
}: {
  isPending: boolean;
  onSubmit: (macId: string) => void;
  onCancel: () => void;
}) {
  const [macId, setMacId] = useState("");
  const isValid = MAC_RE.test(macId.trim());

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (isValid) onSubmit(macId.trim());
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor="mac-id">MAC ID</Label>
        <Input
          id="mac-id"
          placeholder="e.g. DEVICE001"
          value={macId}
          onChange={(e) => setMacId(e.target.value)}
          className={macId && !isValid ? "border-destructive focus-visible:ring-destructive" : ""}
          required
        />
        {macId && !isValid && (
          <p className="text-xs text-destructive">{MSG.Devices.MAC_HINT}</p>
        )}
      </div>

      <div className="flex justify-end gap-2 pt-1">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending || !isValid}>
          {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
          Register
        </Button>
      </div>
    </form>
  );
}
