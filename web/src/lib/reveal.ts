/**
 * Reveal a section once, as it enters.
 *
 * The craft in a page like Tines' comes substantially from how it moves, and
 * the cheapest honest version of that is: sections arrive rather than being
 * already there. Nothing loops, nothing parallaxes, nothing follows the cursor
 * — those need a scroll library, and 15–30 KB of one against a 100 KB budget is
 * not a trade this page can make for decoration.
 *
 * Three properties that make this safe to put on the landing page:
 *
 *  1. **The content is already there.** The element is visible in the SSR HTML;
 *     this only adds a transform once JavaScript has loaded. With JS off, or if
 *     this never runs, the page is complete — which is the rule the whole
 *     frontend is built on.
 *  2. **It disconnects after firing.** An observer left attached to every
 *     section is work done on every scroll frame forever, for an effect that
 *     happens once.
 *  3. **prefers-reduced-motion is checked at attach time**, so a reader who has
 *     asked for stillness never gets an observer at all.
 */
export function reveal(node: HTMLElement, delay = 0) {
	if (typeof IntersectionObserver === 'undefined') return;
	if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

	node.classList.add('reveal-pending');

	const observer = new IntersectionObserver(
		(entries) => {
			for (const entry of entries) {
				if (!entry.isIntersecting) continue;
				(entry.target as HTMLElement).style.transitionDelay = `${delay}ms`;
				entry.target.classList.remove('reveal-pending');
				observer.disconnect();
			}
		},
		// Fire a little before the element reaches the fold, so the movement has
		// finished by the time it is properly in view. An element that starts
		// animating only once fully visible reads as late rather than as arriving.
		{ rootMargin: '0px 0px -12% 0px', threshold: 0.01 }
	);

	observer.observe(node);
	return { destroy: () => observer.disconnect() };
}
