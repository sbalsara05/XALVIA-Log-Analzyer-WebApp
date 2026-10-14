import { useAuth } from "@/contexts/AuthContext";
import { useQuery } from "@tanstack/react-query";
import { orgsService } from "@/services/orgs";
import { Building2 } from "lucide-react";

interface HeaderProps {
  title: string;
}

export function Header({ title }: HeaderProps) {
  const { user } = useAuth();

  const { data: orgs } = useQuery({
    queryKey: ["orgs"],
    queryFn: orgsService.list,
    enabled: !!user,
  });

  // The first org returned is the caller's root admin org
  const primaryOrg = orgs?.[0];

  return (
    <header className="flex h-14 flex-shrink-0 items-center justify-between border-b border-border bg-card px-6">
      <h1 className="text-base font-semibold text-foreground">{title}</h1>

      <div className="flex items-center gap-4">
        {primaryOrg && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Building2 className="h-3.5 w-3.5" />
            <span className="font-medium text-foreground">{primaryOrg.name}</span>
            {primaryOrg.parent_org_id == null && (
              <span className="text-muted-foreground">· Root Org</span>
            )}
          </div>
        )}
        <div className="flex items-center gap-2">
          <div className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
            {user?.email?.[0]?.toUpperCase() ?? "?"}
          </div>
          <span className="text-sm text-muted-foreground hidden sm:block">
            {user?.email}
          </span>
        </div>
      </div>
    </header>
  );
}
