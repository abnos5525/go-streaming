const raw = import.meta.env.VITE_API_BASE_URL as string | undefined;

export const apiBaseURL = (raw?.trim() || "http://127.0.0.1:8085").replace(/\/$/, "");