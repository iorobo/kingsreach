// Lazily-loaded chunk: the glTF loader is a sizeable slice of Babylon and only
// the model-based environments need it, so it is reached through a dynamic
// import rather than sitting in the main path.
import "@babylonjs/loaders/glTF/2.0";

import { SceneLoader, TransformNode, Vector3 } from "./babylon";
import type { AbstractMesh, Scene } from "./babylon";

export interface ModelPlacement {
  /** Fit the model's larger horizontal extent to this many world units. */
  targetWidth: number;
  /** Where `anchor` ends up in world space. */
  position: Vector3;
  rotationY?: number;
  /**
   * A point in the model's own measured frame (unscaled, unrotated) that
   * should land on `position`. Defaults to the centre of its footprint at
   * ground level. Use it to stand somewhere *inside* a model rather than
   * placing the whole thing as one object.
   */
  anchor?: Vector3;
  /** Source files exported from Z-up tools (Source engine maps, CAD) lie on
   * their side otherwise. Applied before measuring, so bounds and anchors are
   * always in the upright frame. */
  zUp?: boolean;
}

export interface LoadedModel {
  meshes: AbstractMesh[];
  /** World-space bounds after placement. */
  min: Vector3;
  max: Vector3;
  /** Scale factor applied to the source model. */
  scale: number;
}

function boundsOf(meshes: AbstractMesh[]): { min: Vector3; max: Vector3 } {
  let min = new Vector3(Infinity, Infinity, Infinity);
  let max = new Vector3(-Infinity, -Infinity, -Infinity);
  for (const m of meshes) {
    m.computeWorldMatrix(true);
    const b = m.getBoundingInfo().boundingBox;
    min = Vector3.Minimize(min, b.minimumWorld);
    max = Vector3.Maximize(max, b.maximumWorld);
  }
  return { min, max };
}

/**
 * Loads a .glb and places it by measured bounds — source files carry arbitrary
 * units (this one came out of a 2018 FBX export), so nothing here may assume a
 * scale. `cancelled` lets a fast environment switch discard a load in flight.
 */
export async function loadGlb(
  scene: Scene,
  parent: TransformNode,
  file: string,
  opts: ModelPlacement,
  cancelled: () => boolean,
): Promise<LoadedModel> {
  const result = await SceneLoader.ImportMeshAsync("", "models/", file, scene);
  const drawn = result.meshes.filter((m) => m.getTotalVertices() > 0);
  if (cancelled()) {
    result.meshes.forEach((m) => m.dispose());
    return { meshes: [], min: Vector3.Zero(), max: Vector3.Zero(), scale: 1 };
  }

  // Own wrapper for the transform: the glTF root carries the loader's
  // handedness fix (as a quaternion, which silently ignores euler rotation),
  // so never write scale/rotation onto it directly.
  const wrapper = new TransformNode("model-" + file, scene);
  wrapper.parent = parent;
  // Orientation sits on its own node so the measured frame is already upright.
  const orient = new TransformNode("orient-" + file, scene);
  orient.parent = wrapper;
  if (opts.zUp) orient.rotation.x = -Math.PI / 2;
  const gltfRoot = result.meshes.find((m) => !m.parent) ?? result.meshes[0]!;
  gltfRoot.parent = orient;

  const measured = boundsOf(drawn); // wrapper is still identity here
  const size = measured.max.subtract(measured.min);
  const scale = opts.targetWidth / Math.max(size.x, size.z, 0.001);
  const rotY = opts.rotationY ?? 0;

  wrapper.scaling.setAll(scale);
  wrapper.rotation.y = rotY;

  const anchor =
    opts.anchor ??
    new Vector3((measured.min.x + measured.max.x) / 2, measured.min.y, (measured.min.z + measured.max.z) / 2);
  const s = anchor.scale(scale);
  const cos = Math.cos(rotY);
  const sin = Math.sin(rotY);
  const rotated = new Vector3(s.x * cos + s.z * sin, s.y, -s.x * sin + s.z * cos);
  wrapper.position = opts.position.subtract(rotated);

  for (const m of drawn) m.isPickable = false; // scenery must never eat a click

  const placed = boundsOf(drawn);
  return { meshes: drawn, min: placed.min, max: placed.max, scale };
}
