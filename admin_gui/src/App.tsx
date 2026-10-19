import {
  Navigate,
  Route,
  Routes,
} from "react-router-dom";

import { AuthGuard } from "@/components/AuthGuard";
import { LoginView } from "@/views/LoginView";

import { signOut } from "firebase/auth";
import { auth } from "@/lib/firebase";

function ProtectedHome() {
  async function handleLogout() {
    await signOut(auth);
  }

  return (
    <div style={{ padding: "2rem" }}>
      <h1>XALIVA Log Analyzer</h1>
      <p>Authentication succeeded.</p>

      <button onClick={handleLogout}>
        Sign out
      </button>
    </div>
  );
}

export default function App() {
  return (
    <Routes>
      <Route
        path="/login"
        element={<LoginView />}
      />

      <Route element={<AuthGuard />}>
        <Route
          path="/"
          element={<ProtectedHome />}
        />
      </Route>

      <Route
        path="*"
        element={<Navigate to="/" replace />}
      />
    </Routes>
  );
}