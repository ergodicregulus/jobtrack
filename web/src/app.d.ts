// See https://svelte.dev/docs/kit/types#app
declare global {
  namespace App {
    interface Locals {
      /** Resolved from the jt_theme cookie in hooks.server.ts. */
      theme: 'system' | 'light' | 'dark';
    }
  }
}

export {};
