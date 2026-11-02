import "server-only";
import { cache } from "react";
import { getMachines, getModel } from "@/lib/api";
import { requireViewer } from "@/lib/auth";

/** Per-request loaders, shared by the layout and the page so the API is called once. */
export const loadViewer = cache(requireViewer);
export const loadModel = cache(async () => getModel(await loadViewer()));
export const loadMachines = cache(async () => getMachines(await loadViewer()));
