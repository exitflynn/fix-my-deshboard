// Front page: the small logo in the top bar stays hidden while the big one is on screen,
// and fades in once it scrolls out of view.
(() => {
  const root = document.documentElement;
  const mark = document.querySelector(".hero-mark");
  if (!mark || !("IntersectionObserver" in window)) {
    root.classList.remove("hero-visible");
    return;
  }
  const bar = document.querySelector(".topbar");
  const offset = bar ? bar.offsetHeight : 0;
  new IntersectionObserver(([entry]) => root.classList.toggle("hero-visible", entry.isIntersecting), {
    rootMargin: `-${offset}px 0px 0px 0px`,
  }).observe(mark);
})();
