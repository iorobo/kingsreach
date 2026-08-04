// Background music.
//
// Adding a track: drop the file in client/static/audio/ and add a line to
// TRACKS below. Nothing else needs touching — the playlist length, the
// shuffling and the "now playing" line all follow from this array.
//
// `licence` is not decoration. Kevin MacLeod's catalogue is Creative Commons
// Attribution, which *requires* the credit to be shown, so the UI reads these
// fields rather than a hard-coded string. Keep them accurate for new tracks.

export interface Track {
  title: string;
  artist: string;
  src: string;
  licence: string;
  /** Where the track came from, for the credits line. */
  source?: string;
}

export const TRACKS: Track[] = [
  {
    title: "Magic Escape Room",
    artist: "Kevin MacLeod",
    src: "audio/magic-escape-room.mp3",
    licence: "CC BY 4.0",
    source: "incompetech.com",
  },
];

const STORE_MUTED = "kr_music_muted";
const VOLUME = 0.34; // background, not foreground

export class Music {
  private readonly audio = new Audio();
  private index = 0;
  private muted: boolean;
  private started = false;
  /** Set once a real user gesture has happened; audio cannot start before. */
  private unlocked = false;
  private onChange: (() => void) | null = null;

  constructor() {
    this.muted = localStorage.getItem(STORE_MUTED) === "1";
    // Never preload: the file is tens of megabytes and streams over range
    // requests. Nobody who leaves the music off should pay for it.
    this.audio.preload = "none";
    this.audio.volume = VOLUME;
    this.audio.addEventListener("ended", () => this.next());
    // A missing or unplayable file must not take the game down with it.
    this.audio.addEventListener("error", () => {
      if (TRACKS.length > 1) this.next();
    });
  }

  /**
   * Browsers refuse to play audio until the player has interacted with the
   * page, so the first click (anywhere) is what actually starts the music.
   */
  attachTo(target: EventTarget): void {
    const unlock = () => {
      this.unlocked = true;
      if (!this.muted) void this.play();
    };
    target.addEventListener("pointerdown", unlock, { once: true });
    target.addEventListener("keydown", unlock, { once: true });
  }

  /** Called when the track or the mute state changes, so the UI can follow. */
  observe(fn: () => void): void {
    this.onChange = fn;
  }

  get isMuted(): boolean {
    return this.muted;
  }

  get current(): Track | null {
    return TRACKS[this.index] ?? null;
  }

  /** Every track, for a credits list. */
  get playlist(): readonly Track[] {
    return TRACKS;
  }

  toggleMute(): void {
    this.muted = !this.muted;
    localStorage.setItem(STORE_MUTED, this.muted ? "1" : "0");
    if (this.muted) {
      this.audio.pause();
    } else if (this.unlocked) {
      void this.play();
    }
    this.onChange?.();
  }

  next(): void {
    if (!TRACKS.length) return;
    this.index = (this.index + 1) % TRACKS.length;
    this.started = false;
    if (!this.muted && this.unlocked) void this.play();
    this.onChange?.();
  }

  private async play(): Promise<void> {
    const track = this.current;
    if (!track) return;
    if (!this.started) {
      this.audio.src = track.src;
      this.audio.loop = TRACKS.length === 1; // a lone track just keeps going
      this.started = true;
    }
    try {
      await this.audio.play();
    } catch {
      // Autoplay refused, or the player muted mid-load. Either way the next
      // gesture or the mute button will try again; nothing to report.
    }
    this.onChange?.();
  }
}
