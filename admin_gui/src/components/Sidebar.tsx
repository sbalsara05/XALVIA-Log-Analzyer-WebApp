import { NavLink } from "react-router-dom";
import { Building2, Users, Cpu, LogOut, ShieldCheck } from "lucide-react";
import { signOut } from "firebase/auth";
import { auth } from "@/lib/firebase";
import { cn } from "@/lib/utils";
import { useAuth } from "@/contexts/AuthContext";

const navItems = [
  { to: "/orgs", label: "Organizations", icon: Building2 },
  { to: "/users", label: "Users", icon: Users },
  { to: "/devices", label: "Devices", icon: Cpu },
];

export function Sidebar() {
  const { user } = useAuth();

  return (
    <aside className="flex h-screen w-56 flex-shrink-0 flex-col border-r border-sidebar-border bg-sidebar text-sidebar-foreground">
      {/* Logo */}
      <div className="flex h-14 items-center gap-2.5 border-b border-sidebar-border px-5">
        <ShieldCheck className="h-5 w-5 text-primary" />
        <div className="flex flex-col leading-none">
          <span className="text-sm font-semibold tracking-tight text-white">Ricovr</span>
          <span className="text-[10px] text-sidebar-foreground/50 tracking-wide uppercase">Admin Console</span>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 overflow-y-auto px-3 py-4 space-y-0.5">
        {navItems.map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
                isActive
                  ? "bg-sidebar-accent text-white"
                  : "text-sidebar-foreground hover:bg-sidebar-accent/60 hover:text-white"
              )
            }
          >
            <Icon className="h-4 w-4 flex-shrink-0" />
            {label}
          </NavLink>
        ))}
      </nav>

      {/* User + Logout */}
      <div className="border-t border-sidebar-border p-3">
        <div className="mb-2 px-3 py-1">
          <p className="text-xs text-sidebar-foreground/60 truncate">{user?.email}</p>
        </div>
        <button
          onClick={() => signOut(auth)}
          className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-sidebar-foreground/70 transition-colors hover:bg-destructive/20 hover:text-red-400"
        >
          <LogOut className="h-4 w-4" />
          Sign out
        </button>
      </div>
    </aside>
  );
}
