// Attribution for everything in the game that somebody else made.
//
// This is not a courtesy list. The imported models and the music are all
// Creative Commons Attribution, and CC-BY requires the credit to be shown to
// the people using the work — a line in the README nobody reads does not
// discharge it. So the Collection screen renders this array, and adding an
// asset means adding a row here.
//
// A complete CC-BY credit names the title, the author, the licence and where
// it came from. Fill in all four.

export interface Credit {
  title: string;
  author: string;
  licence: string;
  /** Where it came from — site name, shown after the licence. */
  source?: string;
  /** Link back to the original, as CC-BY asks for where practical. */
  url?: string;
}

/** Models and scenery. Music lives in music.ts, next to the playlist. */
export const ASSET_CREDITS: Credit[] = [
  {
    title: "de_dust2 - CS map",
    author: "pancakesbassoondonut",
    licence: "CC BY 4.0",
    source: "Sketchfab",
    url: "https://sketchfab.com/3d-models/de-dust2-cs-map-056008d59eb849a29c0ab6884c0c3d87",
  },
  {
    title: "Dice",
    author: "tnRaro",
    licence: "CC BY 4.0",
    source: "Sketchfab",
    url: "https://sketchfab.com/3d-models/dice-3b955af797e140eca0947ede57f412ba",
  },
];

/** One line of credit, as a link where we have one. */
export function creditLine(c: Credit): HTMLElement {
  const row = document.createElement("div");
  const label = `“${c.title}” by ${c.author} — ${c.licence}${c.source ? ` (${c.source})` : ""}`;
  if (c.url) {
    const link = document.createElement("a");
    link.href = c.url;
    link.target = "_blank";
    link.rel = "noopener noreferrer";
    link.textContent = label;
    row.appendChild(link);
  } else {
    row.textContent = label;
  }
  return row;
}
