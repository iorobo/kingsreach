// What happens when a game ends.
//
// Winning drops a crown onto the board and lights the place up. Losing gets
// its own moment too — not a punishment, but not nothing either: the same
// crown, tarnished, tipping over on your own square while the board goes
// dark. A result screen that treats both outcomes the same makes winning feel
// like a dialog box.
//
// The loser's version deliberately does not depend on the king still being on
// the board. Most losses *are* the king being taken, and by the time this runs
// that piece is gone — an animation that topples it would play to an empty
// square in the common case.

import { Color3, Color4, ParticleSystem, SceneLoader, TransformNode, Vector3 } from "./babylon";
import type { Scene } from "./babylon";
import { C, hexPrism, mat, softDot } from "./theme";

const CROWN_MODEL = "king-crown.glb";

/** The handful of PBR fields the crown is re-coloured through. Structural, so
 *  this file does not have to pull the material class into the bundle. */
interface Gildable {
  albedoColor?: Color3;
  emissiveColor?: Color3;
  metallic?: number | null;
  roughness?: number | null;
}

export class Finale {
  private root: TransformNode | null = null;
  private stop: (() => void)[] = [];
  /** Bumped by clear(), so work that was already in flight knows to give up. */
  private generation = 0;

  constructor(private readonly scene: Scene) {}

  /** Clears anything a previous ending left behind. */
  clear(): void {
    this.generation++;
    for (const off of this.stop) off();
    this.stop = [];
    this.root?.dispose(false, true);
    this.root = null;
  }

  /**
   * The winner's crown: it falls in, lands with a bounce and a burst of gold,
   * then hangs above the board and turns. The model is fetched on demand —
   * most games are still in progress, and nobody should pay for an ending they
   * have not reached.
   */
  async triumph(at: Vector3): Promise<void> {
    this.clear();
    const scene = this.scene;
    const era = this.generation;
    await import("./gltf"); // registers the .glb loader plugin
    if (this.generation !== era) return;

    const root = new TransformNode("triumph", scene);
    this.root = root;

    const sparks = this.sparkle(at.add(new Vector3(0, 1.2, 0)), C.goldPale, 260, "up");
    this.stop.push(() => this.fadeOut(sparks));

    // The crown hangs off its own node so the animation moves one thing;
    // writing to the glTF root directly is what its own loader warns against.
    const crown = new TransformNode("crown-carrier", scene);
    crown.parent = root;
    crown.position = at.add(new Vector3(0, 2.6, 0));

    // No model, no crown — the sparks and the fanfare still make the moment.
    const loaded = await this.mountCrown(crown, era, false);
    if (this.generation !== era) return;

    const landAt = at.y + 1.15;
    const from = at.y + 2.6;
    let t = 0;
    let landed = false;
    let burst = false;
    const observer = scene.onBeforeRenderObservable.add(() => {
      t += scene.getEngine().getDeltaTime() / 1000;
      if (!loaded) return;
      // Fall, land with a small bounce, then hang there and turn.
      const drop = Math.min(1, t / 0.85);
      const eased = 1 - Math.pow(1 - drop, 3);
      const bounce = drop >= 1 ? Math.sin((t - 0.85) * 6) * 0.06 * Math.exp(-(t - 0.85) * 2) : 0;
      crown.position.y = from + (landAt - from) * eased + bounce;
      crown.rotation.y += 0.012;
      if (drop >= 1 && !landed) {
        landed = true;
        burst = true;
        sparks.manualEmitCount = 180; // the moment it touches down
      } else if (burst) {
        // A non-negative manualEmitCount puts the system in one-shot mode and
        // silences emitRate for good, so hand the shimmer back after the burst
        // has been consumed — this frame's render already spent it.
        burst = false;
        sparks.manualEmitCount = -1;
      }
    });
    this.stop.push(() => scene.onBeforeRenderObservable.remove(observer));
  }

  /**
   * The loser's moment: the light goes out of the board, a dull crown drops
   * onto your square and rolls onto its side, and your king — if it is still
   * standing — goes over with it. Brief, and finished before it turns into
   * rubbing it in.
   */
  async defeat(at: Vector3, king: TransformNode | null): Promise<void> {
    this.clear();
    const scene = this.scene;
    const era = this.generation;
    await import("./gltf");
    if (this.generation !== era) return;

    const root = new TransformNode("defeat", scene);
    this.root = root;

    // A slab of shadow settling over the board. Cut to the cloth's own hex
    // (radius 5.4 in board.ts) rather than a disc, or it spills over the rim
    // and reads as a dark puddle on the ground. It lies under the pieces, so
    // the board darkens around them rather than swallowing them.
    const pallMat = mat(scene, "pall", Color3.Black(), { alpha: 0, unlit: true });
    const pall = hexPrism(scene, "pall", 5.36, 0.02, pallMat);
    pall.parent = root;
    pall.position.y = 0.011;
    pall.isPickable = false;

    const dust = this.sparkle(at.add(new Vector3(0, 1.5, 0)), new Color3(0.45, 0.44, 0.42), 90, "down");
    this.stop.push(() => this.fadeOut(dust));

    const crown = new TransformNode("crown-fallen", scene);
    crown.parent = root;
    crown.position = at.add(new Vector3(0, 1.3, 0));
    // Rolling on z tips the crown toward its own local −x, so yaw the carrier
    // to aim that at the Throne: a king's square is near the rim, and left to
    // itself the crown falls off the edge of the board.
    if (at.length() > 0.5) crown.rotation.y = Math.atan2(-at.z, at.x);
    const loaded = await this.mountCrown(crown, era, true);
    if (this.generation !== era) return;

    const from = at.y + 1.3;
    const rests = at.y + 0.1;
    const startTilt = king?.rotation.z ?? 0;
    let t = 0;
    const observer = scene.onBeforeRenderObservable.add(() => {
      t += scene.getEngine().getDeltaTime() / 1000;
      pallMat.alpha = Math.min(0.55, t * 0.5);
      if (king) {
        // Topple, and stop once it is lying down.
        king.rotation.z = startTilt + Math.min(Math.PI / 2, t * 1.6);
        king.position.y = Math.max(0, 0.16 - t * 0.12);
      }
      if (!loaded) return;
      const drop = Math.min(1, t / 0.7);
      crown.position.y = from + (rests - from) * (1 - Math.pow(1 - drop, 3));
      // It tips over its own base — the carrier's origin sits there — and
      // rocks briefly before it settles.
      const tip = Math.min(1, Math.max(0, (t - 0.45) / 0.9));
      const rock = tip >= 1 ? 0 : Math.sin(tip * 16) * 0.1 * (1 - tip);
      crown.rotation.z = (Math.PI / 2) * (1 - Math.pow(1 - tip, 2)) + rock;
    });
    this.stop.push(() => scene.onBeforeRenderObservable.remove(observer));
  }

  /**
   * Loads the crown under `carrier`, scaled and standing on the carrier's
   * origin.
   *
   * Deliberately not `loadGlb`: that one measures in world space and assumes
   * its parent sits at the origin, which is true of the scenery it was written
   * for and false of a node this animation moves around. Handing it a moving
   * parent put the crown under the board.
   */
  private async mountCrown(carrier: TransformNode, era: number, dull: boolean): Promise<boolean> {
    const scene = this.scene;
    let meshes;
    try {
      ({ meshes } = await SceneLoader.ImportMeshAsync("", "models/", CROWN_MODEL, scene));
    } catch (e) {
      console.warn("the crown could not be loaded:", e);
      return false;
    }
    const drawn = meshes.filter((m) => m.getTotalVertices() > 0);
    if (!drawn.length || this.generation !== era) {
      meshes.forEach((m) => m.dispose());
      return false;
    }

    // Measure before parenting, while the model is still where the loader put
    // it, then scale to a sensible size and centre it on the carrier.
    let min = new Vector3(Infinity, Infinity, Infinity);
    let max = new Vector3(-Infinity, -Infinity, -Infinity);
    for (const m of drawn) {
      m.computeWorldMatrix(true);
      const bb = m.getBoundingInfo().boundingBox;
      min = Vector3.Minimize(min, bb.minimumWorld);
      max = Vector3.Maximize(max, bb.maximumWorld);
    }
    const size = max.subtract(min);
    const scale = 1.4 / Math.max(size.x, size.z, 0.001);

    // An inner node carries the offset that stands the model on the carrier's
    // origin, so the carrier itself stays a clean handle for position and spin.
    const centred = new TransformNode("crown-centre", scene);
    centred.parent = carrier;
    centred.scaling.setAll(scale);
    const middle = min.add(max).scale(0.5);
    centred.position = new Vector3(-middle.x * scale, -min.y * scale, -middle.z * scale);

    const gltfRoot = meshes.find((m) => !m.parent) ?? meshes[0]!;
    gltfRoot.parent = centred;
    for (const m of drawn) {
      m.isPickable = false;
      this.recolour(m.material as unknown as Gildable | null, dull);
    }
    return true;
  }

  /**
   * The model ships dull olive metal and near-black stones, which under this
   * scene's light reads as brown plastic. Gild it — and give it emissive, or
   * the scene's glow layer has nothing to catch. `dull` is the loser's copy:
   * the same crown with the shine taken out of it.
   */
  private recolour(pbr: Gildable | null, dull: boolean): void {
    if (!pbr?.albedoColor) return;
    const stone = pbr.albedoColor.r > pbr.albedoColor.g * 2; // the only red parts
    if (dull) {
      pbr.albedoColor = stone ? new Color3(0.24, 0.12, 0.13) : new Color3(0.3, 0.27, 0.22);
      pbr.emissiveColor = new Color3(0, 0, 0);
      pbr.metallic = 0.3;
      pbr.roughness = 0.85;
      return;
    }
    if (stone) {
      pbr.albedoColor = new Color3(0.74, 0.07, 0.11);
      pbr.emissiveColor = new Color3(0.3, 0.02, 0.03);
    } else {
      pbr.albedoColor = new Color3(1, 0.78, 0.3);
      pbr.emissiveColor = new Color3(0.34, 0.23, 0.06);
    }
    pbr.metallic = 0.85;
    pbr.roughness = 0.3;
  }

  /** Gold thrown upward, or ash drifting down. Returns the running system. */
  private sparkle(at: Vector3, colour: Color3, count: number, way: "up" | "down"): ParticleSystem {
    const up = way === "up";
    const system = new ParticleSystem(up ? "triumph" : "defeat", count, this.scene);
    system.particleTexture = softDot(this.scene);
    system.emitter = at;
    system.minEmitBox = new Vector3(-0.6, -0.2, -0.6);
    system.maxEmitBox = new Vector3(0.6, 0.6, 0.6);
    system.color1 = new Color4(colour.r, colour.g, colour.b, 1);
    system.color2 = up ? new Color4(1, 0.86, 0.45, 1) : new Color4(0.3, 0.29, 0.28, 0.8);
    system.colorDead = up ? new Color4(0.5, 0.4, 0.15, 0) : new Color4(0.15, 0.15, 0.15, 0);
    // Sized for a board seen from across the room, and against the pale
    // outdoor backdrops: additive gold on bright green needs bulk to read at
    // all, which the first pass (0.04–0.13) did not have.
    system.minSize = up ? 0.09 : 0.05;
    system.maxSize = up ? 0.26 : 0.14;
    system.minLifeTime = up ? 0.7 : 1.4;
    system.maxLifeTime = up ? 1.8 : 2.6;
    system.emitRate = up ? 140 : 26;
    system.blendMode = up ? ParticleSystem.BLENDMODE_ADD : ParticleSystem.BLENDMODE_STANDARD;
    system.gravity = new Vector3(0, up ? -1.4 : -0.35, 0);
    system.direction1 = up ? new Vector3(-1.4, 2.4, -1.4) : new Vector3(-0.25, -0.1, -0.25);
    system.direction2 = up ? new Vector3(1.4, 3.4, 1.4) : new Vector3(0.25, 0.2, 0.25);
    system.minEmitPower = up ? 0.6 : 0.1;
    system.maxEmitPower = up ? 1.8 : 0.4;
    system.updateSpeed = 0.016;
    system.start();
    return system;
  }

  /** Stops emitting, then disposes once the last particles have died, so the
   *  effect ends instead of blinking off. */
  private fadeOut(system: ParticleSystem): void {
    system.stop();
    setTimeout(() => system.dispose(), 3000);
  }
}
