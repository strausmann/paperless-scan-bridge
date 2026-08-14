/*
 * Homepage-only progressive enhancement — loaded exclusively by
 * overrides/home.html. The page is fully legible and functional with
 * this script entirely absent (no critical behaviour depends on it).
 *
 * The one authored moment: the hardware-chain rail draws itself in when
 * it first scrolls into view, instead of appearing instantly. Respects
 * prefers-reduced-motion. No other motion on this page.
 */
(function () {
  "use strict";

  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var rail = document.querySelector(".mdx-chain__rail");
  if (!rail || reduce || !("IntersectionObserver" in window)) return;

  var length = 0;
  try {
    length = rail.getTotalLength();
  } catch (e) {
    return;
  }
  if (!length) return;

  // Draw in as a solid line, then hand back to the resting dotted pattern
  // (set as a presentation attribute on the element, "2 8") once the draw
  // finishes — otherwise the rail would be left solid instead of dotted.
  var restingDasharray = rail.getAttribute("stroke-dasharray") || "2 8";
  rail.style.strokeDasharray = length;
  rail.style.strokeDashoffset = length;
  rail.style.transition = "stroke-dashoffset 900ms cubic-bezier(0.16, 1, 0.3, 1)";
  rail.addEventListener(
    "transitionend",
    function () {
      rail.style.transition = "";
      rail.style.strokeDasharray = restingDasharray;
      rail.style.strokeDashoffset = "0";
    },
    { once: true }
  );

  var observer = new IntersectionObserver(
    function (entries) {
      entries.forEach(function (entry) {
        if (entry.isIntersecting) {
          rail.style.strokeDashoffset = "0";
          observer.disconnect();
        }
      });
    },
    { threshold: 0.4 }
  );
  observer.observe(rail);
})();
