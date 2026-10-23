import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { orgsService, type Org } from "@/services/orgs";

interface UseOrgSelectorResult {
  orgs: Org[];
  selectedOrgId: string;
  setSelectedOrgId: (id: string) => void;
  isError: boolean;
}

// Shared hook for views that need an org picker with auto-selection of the first org.
// Mirrors the pattern in src/mvc/model.py: data ownership lives in one place,
// views just consume the result.
export function useOrgSelector(): UseOrgSelectorResult {
  const [selectedOrgId, setSelectedOrgId] = useState<string>("");

  const { data: orgs = [], isError } = useQuery({
    queryKey: ["orgs"],
    queryFn: orgsService.list,
  });

  useEffect(() => {
    if (!selectedOrgId && orgs.length > 0) {
      setSelectedOrgId(String(orgs[0].id));
    }
  }, [orgs, selectedOrgId]);

  return { orgs, selectedOrgId, setSelectedOrgId, isError };
}
