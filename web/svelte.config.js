import adapter from '@sveltejs/adapter-node';

/** @type {import('@sveltejs/kit').Config} */
export default {
  kit: {
    // adapter-node, not a platform adapter: the deployment target must stay a
    // plain OCI container so this runs on any Kubernetes without a vendor
    // runtime. See docs/architecture/service-topology.md §8.
    adapter: adapter({ out: 'build' }),

    csp: {
      // No 'unsafe-inline'. SvelteKit hashes its own inline bootstrap, and the
      // theme script in app.html is nonce-free by being hash-eligible too.
      mode: 'auto',
      directives: {
        'default-src': ['self'],
        'script-src': ['self'],
        'style-src': ['self', 'unsafe-inline'], // scoped component styles
        'img-src': ['self', 'data:'],
        'connect-src': ['self'],
        'frame-ancestors': ['none'],
        'base-uri': ['self'],
        'form-action': ['self']
      }
    },

    // The API sets its own CSRF protection. SvelteKit's origin check stays on
    // for form actions served from this origin; trustedOrigins replaced the
    // deprecated checkOrigin flag.
    csrf: { trustedOrigins: [] }
  }
};
