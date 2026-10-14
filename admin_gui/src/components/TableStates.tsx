import { Loader2, AlertCircle, type LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Plus } from "lucide-react";

interface LoadingRowProps {
  colSpan?: number;
}

export function LoadingRow({ colSpan }: LoadingRowProps) {
  return (
    <tr>
      <td colSpan={colSpan ?? 99}>
        <div className="flex h-48 items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      </td>
    </tr>
  );
}

interface QueryErrorStateProps {
  message?: string;
}

export function QueryErrorState({ message = "Failed to load data. Please refresh the page." }: QueryErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <AlertCircle className="h-8 w-8 text-destructive/50" />
      <p className="text-sm text-destructive">{message}</p>
    </div>
  );
}

interface SelectOrgPromptProps {
  icon: LucideIcon;
  message: string;
}

export function SelectOrgPrompt({ icon: Icon, message }: SelectOrgPromptProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
      <Icon className="h-8 w-8 text-muted-foreground/50" />
      <p className="text-sm text-muted-foreground">{message}</p>
    </div>
  );
}

interface EmptyStateProps {
  icon: LucideIcon;
  title: string;
  body: string;
  addLabel: string;
  onAdd: () => void;
}

export function EmptyState({ icon: Icon, title, body, addLabel, onAdd }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Icon className="h-6 w-6 text-muted-foreground" />
      </div>
      <div>
        <p className="font-medium text-foreground">{title}</p>
        <p className="mt-1 text-sm text-muted-foreground">{body}</p>
      </div>
      <Button size="sm" onClick={onAdd}>
        <Plus className="h-4 w-4" />
        {addLabel}
      </Button>
    </div>
  );
}
