import { initializeApp } from "firebase/app";
import {
  getAuth,
  inMemoryPersistence,
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

// Token lives in JS memory only — nothing written to localStorage or indexedDB.
// Closing the tab or refreshing the page ends the session immediately.
setPersistence(auth, inMemoryPersistence);

export const googleProvider = new GoogleAuthProvider();

export { signInWithEmailAndPassword, signInWithPopup } from "firebase/auth";