// What changed, in words a player cares about.
//
// This is not the git log. Commit messages are written for whoever maintains
// the code; a changelog is written for whoever plays the game, and the two
// almost never want the same sentence. Add a release at the *top* — the first
// entry is the version the game reports.
//
// Dates are the day the work actually landed, taken from the repository.

export interface Release {
  version: string;
  date: string; // ISO, shown localised
  /** Short line for the top of the entry; optional. */
  headline?: string;
  changes: string[];
}

export const RELEASES: Release[] = [
  {
    version: "0.8",
    date: "2026-08-08",
    headline: "An ending worth reaching, and an opponent that sees it coming.",
    changes: [
      "Winning drops a gold crown onto your king, in a shower of sparks, with a fanfare to go with it.",
      "Losing has its own ending now: the board goes dark and a tarnished crown topples onto your square. A result you can see beats a box with a word in it.",
      "The result arrives as a bar along the bottom instead of a box across the middle, so you can actually watch the ending it is announcing.",
      "The computer looks further ahead. Medium considers your reply before it moves — it genuinely did not before — and hard thinks two moves deep for each side. Hard now beats medium nine games in ten.",
      "The matches in progress are staggered rather than all started at once, so there is something half-played to watch instead of six games on move one.",
      "Rejoining a table you had left no longer insists you are already sitting at it.",
    ],
  },
  {
    version: "0.7",
    date: "2026-08-08",
    headline: "Say something, and learn how to play.",
    changes: [
      "Taunts: nine recorded lines you can call out to the table, with a speech bubble so they land even with the sound off. Eight seconds between them and twelve a game, so nobody can drown the board.",
      "A How to play screen, with diagrams drawn from the real board.",
      "The games in progress are real games now — the computer plays a few matches against itself, and you can watch any of them.",
      "A new version can no longer be answered with a cached copy of the last one.",
    ],
  },
  {
    version: "0.6",
    date: "2026-08-07",
    headline: "The computer learned to play, and nobody can stall any more.",
    changes: [
      "The computer plays at easy, medium or hard, and pauses to think before it moves. It used to give away a piece a turn because it never checked whether the stone it had just moved could be taken.",
      "You have 150 seconds to roll or move. Run it out and you forfeit the game — the countdown is in the top bar on your turn.",
      "Watch games in progress, including the computer's own matches against itself.",
      "Rematch button when a game ends. Against the computer it starts straight away.",
      "Five board colours to choose from, free for everyone.",
      "Crystal Court pieces are readable at last — you could not tell a 2 from a 3.",
      "The café lamps get out of the way instead of hanging in front of the board.",
      "Reach the top three of the hall of champions and your king wears a gold crown at the table.",
      "The dice say roll rather than throw.",
    ],
  },
  {
    version: "0.5",
    date: "2026-08-05",
    headline: "A hall of champions, and an honest scoreboard.",
    changes: [
      "Hall of champions: twenty victories earns you a place on it.",
      "Beating the computer counts as a win. It never used to.",
      "Practice became Offline — pass one screen around a group. Nothing is recorded there.",
      "A stone you can take is circled in red; click it to take it, no dragging.",
      "One account, one seat: you can no longer sit down opposite yourself.",
      "Your country flag is worked out from where you connect from, and you can correct it from the menu.",
    ],
  },
  {
    version: "0.4",
    date: "2026-08-04",
    headline: "The board room opened.",
    changes: [
      "Sign in through Steam, or pick a name and play as a guest.",
      "A lobby browser: search tables by name, host or country, sort them, and open your own — with a password if you want it private.",
      "Country flags beside every player.",
      "Background music, with a mute you only have to press once.",
      "Everything borrowed is credited in the Collection screen, as its licences require.",
    ],
  },
  {
    version: "0.3",
    date: "2026-08-04",
    headline: "Two to four players.",
    changes: [
      "Three and four-player tables.",
      "Roll the dice yourself to decide who opens.",
      "Stones you lose are laid out beside the board.",
      "A computer opponent to fill an empty seat.",
    ],
  },
  {
    version: "0.2",
    date: "2026-08-04",
    headline: "In three dimensions.",
    changes: [
      "Pick up and drop stones, with every legal destination lit.",
      "Orbit the board, and a Focus button to swing back to your own side.",
      "Unlockable piece skins and places to play: a meadow, a medieval fair, a boardgame store, a café.",
    ],
  },
  {
    version: "0.1",
    date: "2026-08-03",
    headline: "First playable.",
    changes: [
      "The board, the rules and a game you can finish.",
    ],
  },
];

/** The version the game reports, which is simply the newest release. */
export const VERSION = RELEASES[0]?.version ?? "0.0";
