// The sound and the flash a move makes.
//
// The sounds are synthesised rather than sampled. Three reasons, in order of
// how much they mattered: a stone on cloth and a stone hitting a stone are
// about eighty milliseconds each, and shipping two more audio files for that
// is a poor trade against a bundle that already streams a soundtrack; the
// pitch can be varied per move so twenty moves in a row do not sound like a
// loop; and there is no licence to keep straight.
//
// Everything here follows the music mute. A player who turned the sound off
// did not mean "except for the clicks".

import { Color3, Color4, MeshBuilder, ParticleSystem, Vector3 } from "./babylon";
import type { Scene } from "./babylon";
import { C, mat, softDot } from "./theme";

type Ctor = typeof AudioContext;

/** One shared audio context; browsers cap how many a page may open. */
let ctx: AudioContext | null = null;

function audio(): AudioContext | null {
  if (ctx) return ctx;
  const Ctx: Ctor | undefined =
    window.AudioContext ?? (window as unknown as { webkitAudioContext?: Ctor }).webkitAudioContext;
  if (!Ctx) return null;
  try {
    ctx = new Ctx();
  } catch {
    return null; // no audio device, or the browser refused; the game is silent, not broken
  }
  return ctx;
}

/** Nudges the context awake after the first gesture, as autoplay rules demand. */
export function unlockEffects(): void {
  const a = audio();
  if (a && a.state === "suspended") void a.resume();
}

/**
 * A short percussive note.
 *
 * `thump` is a stone set down: a low sine that drops in pitch, which is what a
 * soft object landing sounds like. `crack` is a strike: noise through a
 * band-pass, which is what two hard objects hitting each other sounds like.
 */
function thump(volume: number, pitch: number): void {
  const a = audio();
  if (!a || a.state === "suspended") return;
  const osc = a.createOscillator();
  const gain = a.createGain();
  osc.type = "sine";
  const t = a.currentTime;
  osc.frequency.setValueAtTime(210 * pitch, t);
  osc.frequency.exponentialRampToValueAtTime(90 * pitch, t + 0.09);
  gain.gain.setValueAtTime(volume, t);
  gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.13);
  osc.connect(gain).connect(a.destination);
  osc.start(t);
  osc.stop(t + 0.15);
}

function crack(volume: number, pitch: number): void {
  const a = audio();
  if (!a || a.state === "suspended") return;
  const t = a.currentTime;
  const length = Math.floor(a.sampleRate * 0.18);
  const buffer = a.createBuffer(1, length, a.sampleRate);
  const data = buffer.getChannelData(0);
  for (let i = 0; i < length; i++) {
    // White noise under a steep decay: the whole sound is the envelope.
    data[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / length, 6);
  }
  const src = a.createBufferSource();
  src.buffer = buffer;
  const band = a.createBiquadFilter();
  band.type = "bandpass";
  band.frequency.value = 1500 * pitch;
  band.Q.value = 1.1;
  const gain = a.createGain();
  gain.gain.setValueAtTime(volume, t);
  gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.18);
  src.connect(band).connect(gain).connect(a.destination);
  src.start(t);

  // A low knock under the noise, so it reads as weight rather than as static.
  thump(volume * 0.7, pitch * 0.8);
}

/**
 * Move and strike effects on the board.
 *
 * Held here rather than in BoardView because they outlive the thing that
 * caused them: a ring keeps expanding for a third of a second after the stone
 * has arrived, and the board is busy rebuilding pieces by then.
 */
export class Effects {
  private live: (() => void)[] = [];
  /** Set by the app; both sounds follow the music mute. */
  muted = false;

  constructor(private readonly scene: Scene) {}

  dispose(): void {
    for (const stop of this.live.splice(0)) stop();
  }

  /** A stone set down: a soft ring on the cloth and a low knock. */
  landed(at: Vector3): void {
    if (!this.muted) thump(0.16, 0.9 + Math.random() * 0.25);
    this.ring(at, C.teal, 0.75, 0.34);
  }

  /** A strike: a sharper ring, a burst of the victim's colour, and a crack. */
  struck(at: Vector3, colour: Color3): void {
    if (!this.muted) crack(0.22, 0.9 + Math.random() * 0.3);
    this.ring(at, colour, 1.15, 0.42);
    this.shatter(at, colour);
  }

  /** An expanding disc that fades as it grows. */
  private ring(at: Vector3, colour: Color3, size: number, seconds: number): void {
    const disc = MeshBuilder.CreateDisc("fx-ring", { radius: 0.5, tessellation: 32 }, this.scene);
    disc.rotation.x = Math.PI / 2;
    disc.position.set(at.x, 0.035, at.z);
    disc.isPickable = false;
    const m = mat(this.scene, "fx-ring", colour, { alpha: 0.55, unlit: true });
    disc.material = m;
    disc.scaling.setAll(0.2);

    let t = 0;
    const obs = this.scene.onBeforeRenderObservable.add(() => {
      t += this.scene.getEngine().getDeltaTime() / 1000;
      const k = Math.min(1, t / seconds);
      disc.scaling.setAll(0.2 + k * size);
      m.alpha = 0.55 * (1 - k);
      if (k >= 1) stop();
    });
    let done = false;
    const stop = () => {
      if (done) return;
      done = true;
      this.scene.onBeforeRenderObservable.remove(obs);
      disc.dispose();
      m.dispose();
      this.live = this.live.filter((f) => f !== stop);
    };
    this.live.push(stop);
    // A wall-clock backstop, not belt and braces: the render loop is driven by
    // requestAnimationFrame, which browsers stop in a hidden tab. Left to the
    // observer alone, a game played in a background tab piles up one ring mesh
    // and material per move and never clears a single one — measured at 26
    // after a couple of dozen plies.
    setTimeout(stop, seconds * 1000 + 400);
  }

  /** A brief spray of chips in the taken stone's colour. */
  private shatter(at: Vector3, colour: Color3): void {
    const sys = new ParticleSystem("fx-shatter", 60, this.scene);
    sys.particleTexture = softDot(this.scene);
    sys.emitter = at.add(new Vector3(0, 0.1, 0));
    sys.minEmitBox = new Vector3(-0.1, 0, -0.1);
    sys.maxEmitBox = new Vector3(0.1, 0.1, 0.1);
    sys.color1 = new Color4(colour.r, colour.g, colour.b, 1);
    sys.color2 = new Color4(colour.r * 1.3, colour.g * 1.3, colour.b * 1.3, 1);
    sys.colorDead = new Color4(colour.r, colour.g, colour.b, 0);
    sys.minSize = 0.04;
    sys.maxSize = 0.11;
    sys.minLifeTime = 0.25;
    sys.maxLifeTime = 0.55;
    sys.gravity = new Vector3(0, -6, 0);
    sys.direction1 = new Vector3(-1.6, 1.6, -1.6);
    sys.direction2 = new Vector3(1.6, 3, 1.6);
    sys.minEmitPower = 0.7;
    sys.maxEmitPower = 1.7;
    sys.updateSpeed = 0.016;
    // One shot: the stone breaks once, it does not keep breaking.
    sys.manualEmitCount = 34;
    sys.start();
    const stop = () => {
      sys.dispose();
      this.live = this.live.filter((f) => f !== stop);
    };
    this.live.push(stop);
    setTimeout(stop, 900);
  }
}
