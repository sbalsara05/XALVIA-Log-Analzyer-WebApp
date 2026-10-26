import { initializeApp } from "firebase/app";

import {
  getAuth,
  browserLocalPersistence,
  setPersistence,
  GoogleAuthProvider,
} from "firebase/auth";

const firebaseConfig = {
  apiKey: import.meta.env.VITE_FIREBASE_API_KEY,
  authDomain: import.meta.env.VITE_FIREBASE_AUTH_DOMAIN,
  projectId: import.meta.env.VITE_FIREBASE_PROJECT_ID,
};

const app = initializeApp(firebaseConfig);

export const auth = getAuth(app);

// Keep the authenticated Firebase session across page refreshes
// and browser restarts. Explicit sign-out or Firebase revocation
// ends the session.
setPersistence(
  auth,
  browserLocalPersistence
);

export const googleProvider =
  new GoogleAuthProvider();

export {
  signInWithEmailAndPassword,
  signInWithPopup,
} from "firebase/auth";