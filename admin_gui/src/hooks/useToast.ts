import { useState, useEffect, useCallback } from "react";

type ToastVariant = "default" | "destructive" | "success";

interface ToastState {
  id: string;
  title: string;
  description?: string;
  variant?: ToastVariant;
  open: boolean;
}

// Singleton state — all useToast() instances share the same array.
let _toasts: ToastState[] = [];
const _subscribers = new Set<() => void>();

function _notify() {
  _subscribers.forEach((fn) => fn());
}

function _addToast(t: Omit<ToastState, "id" | "open">) {
  const id = Math.random().toString(36).slice(2);
  _toasts = [..._toasts, { ...t, id, open: true }];
  _notify();
  setTimeout(() => {
    _toasts = _toasts.map((item) =>
      item.id === id ? { ...item, open: false } : item
    );
    _notify();
  }, 4000);
}

function _dismiss(id: string) {
  _toasts = _toasts.filter((t) => t.id !== id);
  _notify();
}

export function useToast() {
  const [, rerender] = useState(0);

  useEffect(() => {
    const sub = () => rerender((n) => n + 1);
    _subscribers.add(sub);
    return () => {
      _subscribers.delete(sub);
    };
  }, []);

  const toast = useCallback(_addToast, []);
  const dismiss = useCallback(_dismiss, []);

  return { toasts: _toasts, toast, dismiss };
}

// Imperative helper — usable outside React components (e.g. API interceptors).
export function fireToast(t: Omit<ToastState, "id" | "open">) {
  _addToast(t);
}
