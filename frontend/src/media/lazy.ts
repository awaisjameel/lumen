// The file and selection dialogs, kept out of the script the window starts
// with: they are fetched beside it and are there by the time one is opened.
import { defineAsyncComponent } from 'vue'

export const FileTools = defineAsyncComponent(() => import('./FileTools.vue'))
export const BatchTools = defineAsyncComponent(() => import('./BatchTools.vue'))
