<script lang="ts">
  /**
   * This page is never rendered in normal use — sign-out is a POST to this
   * route's action, which redirects. It exists so the route resolves, and so a
   * stray GET (a bookmarked URL, a prefetch) shows something coherent instead
   * of a 404.
   */
</script>

<div class="shell empty">
  <p class="empty-title">You are signed out</p>
  <a class="btn btn-primary" href="/login">Sign in again</a>
</div>
