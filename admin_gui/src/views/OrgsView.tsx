import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2, Building2, Loader2, FolderTree } from "lucide-react";
import { orgsService, type Org } from "@/services/orgs";
import { MSG } from "@/lib/constants";
import { EmptyState, QueryErrorState } from "@/components/TableStates";
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

export function OrgsView() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const [showAdd, setShowAdd] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Org | null>(null);

  const { data: orgs = [], isLoading, isError } = useQuery({
    queryKey: ["orgs"],
    queryFn: orgsService.list,
  });

  const orgMap = Object.fromEntries(orgs.map((o) => [o.id, o.name]));

  const createMutation = useMutation({
    mutationFn: orgsService.create,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orgs"] });
      setShowAdd(false);
      toast({ title: MSG.Orgs.CREATED, variant: "success" });
    },
    onError: (err: Error) => {
      toast({ title: MSG.Orgs.CREATE_FAILED, description: err.message, variant: "destructive" });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => orgsService.delete(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["orgs"] });
      setDeleteTarget(null);
      toast({ title: MSG.Orgs.DELETED, variant: "success" });
    },
    onError: (err: Error) => {
      toast({ title: MSG.Orgs.DELETE_FAILED, description: err.message, variant: "destructive" });
    },
  });

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">{MSG.Orgs.COUNT(orgs.length)}</p>
        <Button size="sm" onClick={() => setShowAdd(true)}>
          <Plus className="h-4 w-4" />
          New Organization
        </Button>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {isLoading ? (
          <div className="flex h-48 items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : isError ? (
          <QueryErrorState message="Failed to load organizations. Please refresh the page." />
        ) : orgs.length === 0 ? (
          <EmptyState
            icon={Building2}
            title={MSG.Orgs.EMPTY_TITLE}
            body={MSG.Orgs.EMPTY_BODY}
            addLabel="New Organization"
            onAdd={() => setShowAdd(true)}
          />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40">
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Name</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Parent Org</th>
                <th className="px-4 py-3 text-right font-medium text-muted-foreground">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {orgs.map((org) => (
                <tr key={org.id} className="hover:bg-muted/20 transition-colors">
                  <td className="px-4 py-3 font-medium">{org.name}</td>
                  <td className="px-4 py-3">
                    {org.parent_org_id ? (
                      <div className="flex items-center gap-1.5">
                        <FolderTree className="h-3.5 w-3.5 text-muted-foreground" />
                        <span>{orgMap[org.parent_org_id] ?? "Ricovr"}</span>
                      </div>
                    ) : (
                      <Badge variant="default">Root Org</Badge>
                    )}
                  </td>
                  <td className="px-4 py-3 text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                      onClick={() => setDeleteTarget(org)}
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

      {/* Add Org Dialog */}
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New Organization</DialogTitle>
            <DialogDescription>
              Create a sub-organization under one you already administer.
            </DialogDescription>
          </DialogHeader>
          <AddOrgForm
            orgs={orgs}
            isPending={createMutation.isPending}
            onSubmit={(data) => createMutation.mutate(data)}
            onCancel={() => setShowAdd(false)}
          />
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(o) => !o && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete "{deleteTarget?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>{MSG.Orgs.DELETE_BODY}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function AddOrgForm({
  orgs,
  isPending,
  onSubmit,
  onCancel,
}: {
  orgs: Org[];
  isPending: boolean;
  onSubmit: (data: { name: string; parent_org_id: number }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [parentId, setParentId] = useState<string>("");

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!parentId) return;
    onSubmit({ name: name.trim(), parent_org_id: Number(parentId) });
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor="org-name">Organization Name</Label>
        <Input
          id="org-name"
          placeholder="e.g. Acme Corp — East Region"
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="parent-org">Parent Organization</Label>
        <Select value={parentId} onValueChange={setParentId} required>
          <SelectTrigger id="parent-org">
            <SelectValue placeholder="Select parent org…" />
          </SelectTrigger>
          <SelectContent>
            {orgs.map((o) => (
              <SelectItem key={o.id} value={String(o.id)}>
                {o.name}
                {o.parent_org_id == null && (
                  <span className="ml-1.5 text-muted-foreground text-xs">(root)</span>
                )}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="flex justify-end gap-2 pt-1">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending || !name.trim() || !parentId}>
          {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
          Create
        </Button>
      </div>
    </form>
  );
}
