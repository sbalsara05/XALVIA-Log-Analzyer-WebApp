import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

// Accepts any non-empty alphanumeric device ID
export const MAC_RE = /^[0-9a-zA-Z]+$/;

export function formatMAC(raw: string): string {
  return raw;
}