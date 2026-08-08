// Saying something to the table without a keyboard.
//
// Each taunt is a recorded line. The *text* is not decoration: it is what
// somebody with their sound off reads instead, so it has to say the same thing
// the voice does — a bubble that disagrees with the audio is two taunts
// wearing one name. Both come from the server's catalogue for that reason.
//
// The cooldown here is only to keep the button honest. The real limit lives on
// the server, because a disabled button has never stopped anybody determined.

import type { TauntOption, TauntSent } from "./api";

const VOLUME = 0.8;

export class Taunts {
  private readonly audio = new Audio();
  private catalogue: TauntOption[] = [];
  private gapMs = 8000;
  private nextAllowedAt = 0;
  /** Nonces already played, so a taunt is not repeated on every poll. */
  private heard = new Set<string>();

  constructor() {
    this.audio.preload = "none";
    this.audio.volume = VOLUME;
  }

  load(options: TauntOption[], gapMs: number): void {
    this.catalogue = options;
    this.gapMs = gapMs;
  }

  get options(): readonly TauntOption[] {
    return this.catalogue;
  }

  /** Milliseconds until the next one may be sent; 0 when ready. */
  cooldownLeft(): number {
    return Math.max(0, this.nextAllowedAt - Date.now());
  }

  /** Called when a send is accepted, so the button matches the server. */
  noteSent(): void {
    this.nextAllowedAt = Date.now() + this.gapMs;
  }

  /** The server refused us; hold the button for a beat rather than retrying. */
  noteRefused(): void {
    this.nextAllowedAt = Date.now() + Math.min(this.gapMs, 3000);
  }

  /**
   * Plays an incoming taunt once. Returns the text to show, or null when this
   * is one we have already heard — the state carries the same taunt for a few
   * seconds so a slow poll cannot miss it, which means most polls see a repeat.
   */
  receive(t: TauntSent | undefined, muted: boolean): TauntSent | null {
    if (!t || this.heard.has(t.nonce)) return null;
    this.heard.add(t.nonce);
    if (this.heard.size > 200) this.heard.clear();

    const option = this.catalogue.find((o) => o.id === t.id);
    if (option && !muted) {
      this.audio.src = option.sound;
      // Autoplay may be refused before the first gesture; the bubble still
      // shows, which is the point of having text at all.
      void this.audio.play().catch(() => {});
    }
    return t;
  }

  /** Forget what we have heard, so a new game starts quiet. */
  reset(): void {
    this.heard.clear();
    this.nextAllowedAt = 0;
  }
}
