import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Pencil, UserMinus, Users, Loader2, ShieldCheck, User } from "lucide-react";
import { usersService, type OrgUser, type UpsertUserPayload, type UpsertUserResponse } from "@/services/users";
import { MSG } from "@/lib/constants";
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
import { useAuth } from "@/contexts/AuthContext";

export function UsersView() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { user: currentUser } = useAuth();
  const { orgs, selectedOrgId, setSelectedOrgId, isError: orgsError } = useOrgSelector();
  const [showAdd, setShowAdd] = useState(false);
  const [editTarget, setEditTarget] = useState<OrgUser | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<OrgUser | null>(null);

  const { data: users = [], isLoading, isError: usersError } = useQuery({
    queryKey: ["users", selectedOrgId],
    queryFn: () => usersService.listByOrg(Number(selectedOrgId)),
    enabled: !!selectedOrgId,
  });

  const upsertMutation = useMutation({
    mutationFn: ({ orgId, payload }: { orgId: number; payload: UpsertUserPayload }) =>
      usersService.upsert(orgId, payload),
    onSuccess: (data: UpsertUserResponse) => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setShowAdd(false);
      setEditTarget(null);
      toast({ title: MSG.Users.SAVED, variant: "success" });
      if (data.email_warning) {
        toast({ title: MSG.Users.INVITE_EMAIL_WARNING, description: data.email_warning, variant: "destructive" });
      }
    },
    onError: (err: Error) => {
      toast({ title: MSG.Users.SAVE_FAILED, description: err.message, variant: "destructive" });
    },
  });

  const revokeMutation = useMutation({
    mutationFn: ({ orgId, userId }: { orgId: number; userId: string }) =>
      usersService.revoke(orgId, userId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setRevokeTarget(null);
      toast({ title: MSG.Users.REMOVED, variant: "success" });
    },
    onError: (err: Error) => {
      toast({ title: MSG.Users.REMOVE_FAILED, description: err.message, variant: "destructive" });
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
          Add User
        </Button>
      </div>

      {/* Table */}
      <div className="rounded-lg border border-border bg-card overflow-hidden">
        {orgsError ? (
          <QueryErrorState message="Failed to load organizations. Please refresh the page." />
        ) : !selectedOrgId ? (
          <SelectOrgPrompt icon={Users} message={MSG.Users.SELECT_ORG_PROMPT} />
        ) : isLoading ? (
          <div className="flex h-48 items-center justify-center">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : usersError ? (
          <QueryErrorState message="Failed to load users. Please refresh the page." />
        ) : users.length === 0 ? (
          <EmptyState
            icon={Users}
            title={MSG.Users.EMPTY_TITLE}
            body={MSG.Users.EMPTY_BODY}
            addLabel="Add User"
            onAdd={() => setShowAdd(true)}
          />
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-muted/40">
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Email</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Role</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Permissions</th>
                <th className="px-4 py-3 text-left font-medium text-muted-foreground">Granted</th>
                <th className="px-4 py-3 text-right font-medium text-muted-foreground">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {users.map((u) => {
                const isSelf = u.user_id === currentUser?.uid;
                return (
                <tr key={u.user_id} className="hover:bg-muted/20 transition-colors">
                  <td className="px-4 py-3 font-medium">{u.email}</td>
                  <td className="px-4 py-3">
                    {u.role === "admin" ? (
                      <Badge variant="default">
                        <ShieldCheck className="h-3 w-3 mr-1" />
                        Admin
                      </Badge>
                    ) : (
                      <Badge variant="secondary">
                        <User className="h-3 w-3 mr-1" />
                        User
                      </Badge>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex gap-1.5">
                      {u.can_read && <Badge variant="outline">Read</Badge>}
                      {u.can_upload && <Badge variant="outline">Upload</Badge>}
                      {!u.can_read && !u.can_upload && (
                        <span className="text-xs text-muted-foreground">None</span>
                      )}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {new Date(u.granted_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-muted-foreground hover:text-foreground"
                        onClick={() => setEditTarget(u)}
                        disabled={isSelf}
                        title={isSelf ? "Cannot edit your own permissions" : undefined}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        onClick={() => setRevokeTarget(u)}
                        disabled={isSelf}
                        title={isSelf ? "Cannot remove yourself" : undefined}
                      >
                        <UserMinus className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {/* Add User Dialog */}
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add User</DialogTitle>
            <DialogDescription>
              Grant a user access to the selected organization.
            </DialogDescription>
          </DialogHeader>
          <UserForm
            isPending={upsertMutation.isPending}
            onSubmit={(payload) =>
              upsertMutation.mutate({ orgId: Number(selectedOrgId), payload })
            }
            onCancel={() => setShowAdd(false)}
          />
        </DialogContent>
      </Dialog>

      {/* Edit Permissions Dialog */}
      <Dialog open={!!editTarget} onOpenChange={(o) => !o && setEditTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit Permissions</DialogTitle>
            <DialogDescription>{editTarget?.email}</DialogDescription>
          </DialogHeader>
          {editTarget && (
            <UserForm
              defaultValues={{
                email: editTarget.email,
                role: editTarget.role,
                can_read: editTarget.can_read,
                can_upload: editTarget.can_upload,
              }}
              lockEmail
              isPending={upsertMutation.isPending}
              onSubmit={(payload) =>
                upsertMutation.mutate({ orgId: Number(selectedOrgId), payload })
              }
              onCancel={() => setEditTarget(null)}
            />
          )}
        </DialogContent>
      </Dialog>

      {/* Revoke Confirmation */}
      <AlertDialog open={!!revokeTarget} onOpenChange={(o) => !o && setRevokeTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Remove "{revokeTarget?.email}"?</AlertDialogTitle>
            <AlertDialogDescription>{MSG.Users.REVOKE_BODY}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() =>
                revokeTarget &&
                revokeMutation.mutate({
                  orgId: Number(selectedOrgId),
                  userId: revokeTarget.user_id,
                })
              }
              disabled={revokeMutation.isPending}
            >
              {revokeMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Remove
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

interface UserFormProps {
  defaultValues?: Partial<UpsertUserPayload>;
  lockEmail?: boolean;
  isPending: boolean;
  onSubmit: (payload: UpsertUserPayload) => void;
  onCancel: () => void;
}

function UserForm({ defaultValues, lockEmail, isPending, onSubmit, onCancel }: UserFormProps) {
  const [email, setEmail] = useState(defaultValues?.email ?? "");
  const [role, setRole] = useState<"admin" | "user">(defaultValues?.role ?? "user");
  const [canRead, setCanRead] = useState(defaultValues?.can_read ?? false);
  const [canUpload, setCanUpload] = useState(defaultValues?.can_upload ?? false);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({ email: email.trim(), role, can_read: canRead, can_upload: canUpload });
  }

  const isAdmin = role === "admin";

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor="user-email">Email Address</Label>
        <Input
          id="user-email"
          type="email"
          placeholder="user@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={lockEmail}
          required
        />
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="user-role">Role</Label>
        <Select value={role} onValueChange={(v) => setRole(v as "admin" | "user")}>
          <SelectTrigger id="user-role">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="user">User</SelectItem>
            <SelectItem value="admin">Admin</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">
          Permissions
          {isAdmin && (
            <span className="ml-2 text-xs text-muted-foreground font-normal">
              {MSG.Users.ADMIN_PERMS_NOTE}
            </span>
          )}
        </legend>
        <div className="space-y-2 pl-0.5">
          <label className="flex items-center gap-2.5 text-sm cursor-pointer">
            <input
              type="checkbox"
              className="h-4 w-4 rounded border-border accent-primary"
              checked={isAdmin || canRead}
              disabled={isAdmin}
              onChange={(e) => setCanRead(e.target.checked)}
            />
            <span className={isAdmin ? "text-muted-foreground" : ""}>Read logs</span>
          </label>
          <label className="flex items-center gap-2.5 text-sm cursor-pointer">
            <input
              type="checkbox"
              className="h-4 w-4 rounded border-border accent-primary"
              checked={isAdmin || canUpload}
              disabled={isAdmin}
              onChange={(e) => setCanUpload(e.target.checked)}
            />
            <span className={isAdmin ? "text-muted-foreground" : ""}>Upload / Write</span>
          </label>
        </div>
      </fieldset>

      <div className="flex justify-end gap-2 pt-1">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={isPending || !email.trim()}>
          {isPending && <Loader2 className="h-4 w-4 animate-spin" />}
          Save
        </Button>
      </div>
    </form>
  );
}
