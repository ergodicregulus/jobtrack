import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],

  server: {
    host: '0.0.0.0',
    port: 5173,
    // Vite rejects Host headers it does not recognise, as a DNS-rebinding
    // defence. Inside Docker the app is reached by its SERVICE NAME ("web"),
    // not localhost, so without this every container-to-container request —
    // including the whole E2E suite — gets "Blocked request".
    //
    // Dev server only. The production build is served by adapter-node, which
    // has no such check.
    allowedHosts: ['web', 'localhost', '127.0.0.1'],
    // The browser reaches Vite on localhost; Vite reaches the API by its
    // compose service name. Proxying rather than calling the API directly from
    // the browser keeps everything same-origin, so the session cookie works
    // without CORS and without SameSite=None.
    proxy: {
      '/v1': {
        target: process.env.API_URL ?? 'http://api:8080',
        changeOrigin: true
      }
    }
  },

  build: {
    // Fail the build if a chunk exceeds the budget rather than warning. A
    // budget that only warns is a budget that gets exceeded.
    chunkSizeWarningLimit: 100,
    rollupOptions: {
      output: {
        // Content-hashed filenames so assets can be cached immutably forever.
        entryFileNames: 'assets/[name].[hash].js',
        chunkFileNames: 'assets/[name].[hash].js',
        assetFileNames: 'assets/[name].[hash].[ext]'
      }
    }
  }
});
