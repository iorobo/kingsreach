// Little country flags, drawn in CSS.
//
// Emoji flags (🇳🇱) are the obvious choice and the wrong one: Windows ships no
// glyphs for regional indicator pairs, so every flag there renders as the bare
// letters. These are layered gradients instead — small, exact enough at 21×14,
// and identical in every browser.

/** Bands running top to bottom. */
function h(...bands: string[]): string {
  return bandGradient("180deg", bands);
}

/** Bands running left to right. */
function v(...bands: string[]): string {
  return bandGradient("90deg", bands);
}

function bandGradient(angle: string, bands: string[]): string {
  const step = 100 / bands.length;
  const stops = bands.map((c, i) => `${c} ${(i * step).toFixed(2)}% ${((i + 1) * step).toFixed(2)}%`);
  return `linear-gradient(${angle}, ${stops.join(", ")})`;
}

/** Three horizontal bands where the middle one is half the flag (Spain, Canada-ish). */
function hWide(edge: string, middle: string): string {
  return `linear-gradient(180deg, ${edge} 0 25%, ${middle} 25% 75%, ${edge} 75% 100%)`;
}

function vWide(edge: string, middle: string): string {
  return `linear-gradient(90deg, ${edge} 0 25%, ${middle} 25% 75%, ${edge} 75% 100%)`;
}

/** A Nordic cross: bar offset towards the hoist. */
function nordic(bg: string, cross: string): string {
  return [
    `linear-gradient(90deg, transparent 0 30%, ${cross} 30% 46%, transparent 46% 100%)`,
    `linear-gradient(180deg, transparent 0 38%, ${cross} 38% 60%, transparent 60% 100%)`,
    bg,
  ].join(", ");
}

/** A disc in the middle (Japan, Bangladesh). */
function disc(bg: string, dot: string, size = 30): string {
  return `radial-gradient(circle at 50% 50%, ${dot} 0 ${size}%, transparent ${size + 1}%), ${bg}`;
}

/** A canton in the top-left corner over a striped field. */
function canton(field: string, block: string): string {
  return `linear-gradient(${block}, ${block}) 0 0 / 42% 54% no-repeat, ${field}`;
}

// Rough but recognisable at chip size; nobody is reading heraldry off 21 pixels.
const FLAGS: Record<string, { name: string; css: string }> = {
  AR: { name: "Argentina", css: h("#74ACDF", "#fff", "#74ACDF") },
  AT: { name: "Austria", css: h("#ED2939", "#fff", "#ED2939") },
  AU: { name: "Australia", css: canton("#00247D", "#0d2f7a") },
  BE: { name: "Belgium", css: v("#000", "#FDDA24", "#EF3340") },
  BG: { name: "Bulgaria", css: h("#fff", "#00966E", "#D62612") },
  BR: { name: "Brazil", css: `radial-gradient(circle at 50% 50%, #002776 0 20%, transparent 21%), linear-gradient(135deg, transparent 26%, #FEDD00 26% 74%, transparent 74%), #009C3B` },
  CA: { name: "Canada", css: vWide("#D80621", "#fff") },
  CH: { name: "Switzerland", css: `linear-gradient(90deg, transparent 0 38%, #fff 38% 62%, transparent 62%), linear-gradient(180deg, transparent 0 32%, #fff 32% 68%, transparent 68%), #DA291C` },
  CL: { name: "Chile", css: `linear-gradient(#0039A6,#0039A6) 0 0 / 38% 50% no-repeat, ${h("#fff", "#D52B1E")}` },
  CN: { name: "China", css: `radial-gradient(circle at 22% 32%, #FFDE00 0 16%, transparent 17%), #EE1C25` },
  CZ: { name: "Czechia", css: `linear-gradient(135deg, #11457E 0 34%, transparent 34%), ${h("#fff", "#D7141A")}` },
  DE: { name: "Germany", css: h("#000", "#DD0000", "#FFCE00") },
  DK: { name: "Denmark", css: nordic("#C60C30", "#fff") },
  EE: { name: "Estonia", css: h("#0072CE", "#000", "#fff") },
  ES: { name: "Spain", css: hWide("#AA151B", "#F1BF00") },
  FI: { name: "Finland", css: nordic("#fff", "#002F6C") },
  FR: { name: "France", css: v("#002395", "#fff", "#ED2939") },
  GB: { name: "United Kingdom", css: `linear-gradient(90deg, transparent 0 40%, #CF142B 40% 60%, transparent 60%), linear-gradient(180deg, transparent 0 38%, #CF142B 38% 62%, transparent 62%), linear-gradient(58deg, transparent 44%, #fff 44% 56%, transparent 56%), linear-gradient(-58deg, transparent 44%, #fff 44% 56%, transparent 56%), #00247D` },
  GH: { name: "Ghana", css: `radial-gradient(circle at 50% 50%, #000 0 15%, transparent 16%), ${h("#CE1126", "#FCD116", "#006B3F")}` },
  GR: { name: "Greece", css: `linear-gradient(#0D5EAF,#0D5EAF) 0 0 / 44% 56% no-repeat, ${h("#0D5EAF", "#fff", "#0D5EAF", "#fff", "#0D5EAF")}` },
  HR: { name: "Croatia", css: h("#FF0000", "#fff", "#171796") },
  HU: { name: "Hungary", css: h("#CD2A3E", "#fff", "#436F4D") },
  IE: { name: "Ireland", css: v("#169B62", "#fff", "#FF883E") },
  IN: { name: "India", css: `radial-gradient(circle at 50% 50%, #000080 0 11%, transparent 12%), ${h("#FF9933", "#fff", "#138808")}` },
  IS: { name: "Iceland", css: nordic("#02529C", "#fff") },
  IT: { name: "Italy", css: v("#008C45", "#F4F5F0", "#CD212A") },
  JP: { name: "Japan", css: disc("#fff", "#BC002D") },
  KR: { name: "South Korea", css: disc("#fff", "#CD2E3A", 22) },
  LT: { name: "Lithuania", css: h("#FDB913", "#006A44", "#C1272D") },
  LU: { name: "Luxembourg", css: h("#ED2939", "#fff", "#00A1DE") },
  LV: { name: "Latvia", css: `linear-gradient(180deg, #9E3039 0 40%, #fff 40% 60%, #9E3039 60% 100%)` },
  MA: { name: "Morocco", css: `radial-gradient(circle at 50% 50%, transparent 0 12%, #006233 13% 20%, transparent 21%), #C1272D` },
  MX: { name: "Mexico", css: v("#006847", "#fff", "#CE1126") },
  NL: { name: "Netherlands", css: h("#AE1C28", "#fff", "#21468B") },
  NO: { name: "Norway", css: `linear-gradient(90deg, transparent 0 28%, #fff 28% 48%, transparent 48%), linear-gradient(180deg, transparent 0 36%, #fff 36% 62%, transparent 62%), ${nordic("#BA0C2F", "#00205B")}` },
  NZ: { name: "New Zealand", css: canton("#00247D", "#0d2f7a") },
  PL: { name: "Poland", css: h("#fff", "#DC143C") },
  PT: { name: "Portugal", css: `radial-gradient(circle at 38% 50%, #FFE900 0 15%, transparent 16%), linear-gradient(90deg, #046A38 0 40%, #DA291C 40% 100%)` },
  RO: { name: "Romania", css: v("#002B7F", "#FCD116", "#CE1126") },
  RS: { name: "Serbia", css: h("#C6363C", "#0C4076", "#fff") },
  SE: { name: "Sweden", css: nordic("#006AA7", "#FECC02") },
  SI: { name: "Slovenia", css: h("#fff", "#0000A0", "#D50000") },
  SK: { name: "Slovakia", css: h("#fff", "#0B4EA2", "#EE1C25") },
  TR: { name: "Türkiye", css: `radial-gradient(circle at 40% 50%, transparent 0 16%, #fff 17% 25%, transparent 26%), #E30A17` },
  UA: { name: "Ukraine", css: h("#0057B7", "#FFDD00") },
  US: { name: "United States", css: canton("#B31942", "#0A3161") },
  ZA: { name: "South Africa", css: `linear-gradient(90deg, #007A4D 0 30%, transparent 30%), ${h("#DE3831", "#fff", "#002395")}` },
};

/** Countries offered in the picker, alphabetically by name. */
export function countryList(): { code: string; name: string }[] {
  return Object.entries(FLAGS)
    .map(([code, f]) => ({ code, name: f.name }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function countryName(code: string): string {
  return FLAGS[code.toUpperCase()]?.name ?? code.toUpperCase();
}

/**
 * A flag chip for a two-letter code. Unknown or missing codes get a neutral
 * chip with the letters, never a broken image.
 */
export function flagChip(code: string): HTMLElement {
  const el = document.createElement("span");
  el.className = "flag";
  const key = (code ?? "").toUpperCase();
  const flag = FLAGS[key];
  if (flag) {
    el.style.background = flag.css;
    el.title = flag.name;
  } else {
    el.classList.add("plain");
    el.textContent = key.slice(0, 2) || "··";
  }
  return el;
}
