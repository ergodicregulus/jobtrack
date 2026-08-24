import { defineConfig } from 'vitest/config';

/**
 * Vitest config is separate from vite.config.ts on purpose.
 *
 * Declaring `test` inside the Vite config requires vitest's own defineConfig,
 * which carries vitest's pinned Vite type definitions. Those conflict with the
 * Vite 6 types the SvelteKit plugin is built against, and the result is a type
 * error in the plugin array that has nothing to do with either config. Keeping
 * them in separate files keeps each one typed by the tool that owns it.
 *
 * Unit tests here cover pure functions only — formatting, parsing, scoring
 * helpers — so no Svelte plugin or DOM environment is needed. Component and
 * flow behaviour is covered by the Playwright suite against a real browser,
 * where it is actually meaningful.
 */
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts']
  }
});
