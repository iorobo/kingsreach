import { SceneLoader, TransformNode, Vector3 } from "./babylon";
import type { AbstractMesh, Mesh, Scene } from "./babylon";
import type { Seat } from "./api";

// The opening roll. The die is a loaded model (client/static/models/dice.glb);
// the player throws it themselves and the server decides the value at that
// moment, so the tumble lands on a real result rather than replaying one.

/**
 * Rotation that brings each face uppermost, read off this particular model by
 * rendering all six candidates side by side. Do not "tidy" these: the model
 * numbers its Z faces the opposite way round from the obvious guess, so 3 and
 * 4 are not where you would expect. Re-measure if the model is ever replaced.
 */
const FACE_UP: Record<number, Vector3> = {
  1: new Vector3(0, 0, 0),
  2: new Vector3(0, 0, Math.PI / 2),
  3: new Vector3(Math.PI / 2, 0, 0),
  4: new Vector3(-Math.PI / 2, 0, 0),
  5: new Vector3(0, 0, -Math.PI / 2),
  6: new Vector3(Math.PI, 0, 0),
};

const DIE_SIZE = 0.85; // world units across, sitting above the board

function ease(t: number): number {
  return 1 - Math.pow(1 - t, 3);
}

export class DiceRoller {
  private template: AbstractMesh[] = [];
  private loading: Promise<void> | null = null;
  private root: TransformNode | null = null;
  private held: { node: TransformNode; seat: Seat } | null = null;
  private idleSpin = 0;
  private idleObserver: unknown = null;

  constructor(private readonly scene: Scene) {}

  /** Loads the model once; every die is a clone of it. */
  private ensureLoaded(): Promise<void> {
    if (this.loading) return this.loading;
    this.loading = (async () => {
      // gltf.ts is what registers the .glb plugin; importing it here keeps that
      // chunk lazy and shared with the model-based environments.
      await import("./gltf");
      const result = await SceneLoader.ImportMeshAsync("", "models/", "dice.glb", this.scene);
      const drawn = result.meshes.filter((m) => m.getTotalVertices() > 0);
      // Normalise: centre on the origin and scale to DIE_SIZE across.
      let min = new Vector3(Infinity, Infinity, Infinity);
      let max = new Vector3(-Infinity, -Infinity, -Infinity);
      for (const m of drawn) {
        m.computeWorldMatrix(true);
        const b = m.getBoundingInfo().boundingBox;
        min = Vector3.Minimize(min, b.minimumWorld);
        max = Vector3.Maximize(max, b.maximumWorld);
      }
      const size = max.subtract(min);
      const scale = DIE_SIZE / Math.max(size.x, size.y, size.z, 0.001);
      const centre = min.add(max).scale(0.5);

      const holder = new TransformNode("dice-template", this.scene);
      holder.setEnabled(false);
      const gltfRoot = result.meshes.find((m) => !m.parent) ?? result.meshes[0]!;
      gltfRoot.parent = holder;
      gltfRoot.position.subtractInPlace(centre);
      holder.scaling.setAll(scale);
      for (const m of drawn) m.isPickable = false;
      this.template = drawn;
      this.templateHolder = holder;
    })();
    return this.loading;
  }

  private templateHolder: TransformNode | null = null;

  private newDie(name: string): TransformNode {
    const node = new TransformNode(name, this.scene);
    node.parent = this.root;
    for (const src of this.template) {
      const copy = (src as Mesh).clone(name + "-part", node);
      if (copy) {
        copy.setEnabled(true);
        copy.isPickable = true;
        copy.metadata = { die: true };
      }
    }
    // Undo the template holder's scale, which the clones do not inherit.
    const s = this.templateHolder?.scaling.x ?? 1;
    node.scaling.setAll(s);
    return node;
  }

  private ensureRoot(): TransformNode {
    if (!this.root) this.root = new TransformNode("dice", this.scene);
    return this.root;
  }

  /**
   * Puts a die in the air, turning gently, waiting to be thrown. The caller
   * throws it with `throwTo` once the server has decided the value.
   */
  async present(seat: Seat, height = 3.2): Promise<void> {
    await this.ensureLoaded();
    this.clear();
    this.ensureRoot();
    const node = this.newDie("die-" + seat);
    node.position.set(0, height, 0);
    node.rotation.set(0.5, 0.4, 0.2);
    this.held = { node, seat };
    this.idleSpin = 0;
    this.idleObserver = this.scene.onBeforeRenderObservable.add(() => {
      if (!this.held) return;
      this.idleSpin += this.scene.getEngine().getDeltaTime() / 1000;
      this.held.node.rotation.y += 0.012;
      this.held.node.position.y = height + Math.sin(this.idleSpin * 1.7) * 0.09;
    });
  }

  /** True when the given mesh belongs to the die waiting to be thrown. */
  isDie(mesh: { metadata?: unknown } | null | undefined): boolean {
    return !!this.held && !!(mesh?.metadata as { die?: boolean } | undefined)?.die;
  }

  get waiting(): boolean {
    return this.held !== null;
  }

  /** Tumbles the waiting die and settles it on `value`. */
  async throwTo(value: number, height = 3.2): Promise<void> {
    if (!this.held) return;
    const { node } = this.held;
    this.stopIdle();
    const target = FACE_UP[value] ?? FACE_UP[1]!;
    const from = node.rotation.clone();
    const spin = { x: 5 + Math.random() * 4, y: 5 + Math.random() * 4, z: 5 + Math.random() * 4 };

    await this.tween(1150, (t) => {
      const e = ease(t);
      node.rotation.set(
        from.x + spin.x * (1 - e) * 6 + (target.x - from.x) * e,
        from.y + spin.y * (1 - e) * 6 + (target.y - from.y) * e,
        from.z + spin.z * (1 - e) * 6 + (target.z - from.z) * e,
      );
      // Throw it up, then let it drop onto the board.
      node.position.y = height + Math.sin(t * Math.PI) * 1.1 - e * 0.35;
    });
    node.rotation.copyFrom(target);
    node.position.y = height - 0.35;
    await this.tween(420, () => {});
  }

  clear(): void {
    this.stopIdle();
    this.held = null;
    this.root?.getChildMeshes().forEach((m) => m.dispose());
    this.root?.dispose();
    this.root = null;
  }

  private stopIdle(): void {
    if (this.idleObserver) {
      this.scene.onBeforeRenderObservable.remove(this.idleObserver as never);
      this.idleObserver = null;
    }
  }

  private tween(ms: number, step: (t: number) => void): Promise<void> {
    return new Promise((resolve) => {
      let elapsed = 0;
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        this.scene.onBeforeRenderObservable.remove(obs);
        clearTimeout(timer);
        resolve();
      };
      const obs = this.scene.onBeforeRenderObservable.add(() => {
        elapsed += this.scene.getEngine().getDeltaTime();
        const t = Math.min(1, elapsed / ms);
        step(t);
        if (t >= 1) finish();
      });
      // A hidden tab produces no frames; never let the intro hang on that.
      const timer = setTimeout(() => {
        step(1);
        finish();
      }, ms + 400);
    });
  }
}
