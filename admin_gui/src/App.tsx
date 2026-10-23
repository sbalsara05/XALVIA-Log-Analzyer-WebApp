import { lazy, Suspense } from "react";
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
} from "react-router-dom";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { AuthProvider } from "@/contexts/AuthContext";
import { AuthGuard } from "@/components/AuthGuard";
import { DashboardLayout } from "@/components/DashboardLayout";
import { Toaster } from "@/components/Toaster";

const LoginView = lazy(() =>
  import("@/views/LoginView").then((m) => ({
    default: m.LoginView,
  }))
);

const OrgsView = lazy(() =>
  import("@/views/OrgsView").then((m) => ({
    default: m.OrgsView,
  }))
);

const UsersView = lazy(() =>
  import("@/views/UsersView").then((m) => ({
    default: m.UsersView,
  }))
);

const DevicesView = lazy(() =>
  import("@/views/DevicesView").then((m) => ({
    default: m.DevicesView,
  }))
);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
});

function RouteSpinner() {
  return (
    <div className="flex h-screen items-center justify-center bg-background">
      <Loader2 className="h-6 w-6 animate-spin text-primary" />
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <Suspense fallback={<RouteSpinner />}>
            <Routes>
              <Route
                path="/login"
                element={<LoginView />}
              />

              <Route element={<AuthGuard />}>
                <Route element={<DashboardLayout />}>
                  <Route
                    index
                    element={
                      <Navigate
                        to="/orgs"
                        replace
                      />
                    }
                  />

                  <Route
                    path="/orgs"
                    element={<OrgsView />}
                  />

                  <Route
                    path="/users"
                    element={<UsersView />}
                  />

                  <Route
                    path="/devices"
                    element={<DevicesView />}
                  />
                </Route>
              </Route>

              <Route
                path="*"
                element={
                  <Navigate
                    to="/orgs"
                    replace
                  />
                }
              />
            </Routes>
          </Suspense>
        </BrowserRouter>

        <Toaster />
      </AuthProvider>
    </QueryClientProvider>
  );
}