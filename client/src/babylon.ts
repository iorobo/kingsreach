// Single place where Babylon.js is pulled in. Deep imports (instead of the
// "@babylonjs/core" barrel) let esbuild tree-shake the engine down to what the
// game actually uses — roughly a quarter of the full bundle.

import "@babylonjs/core/Culling/ray"; // scene.pick / createPickingRay
import "@babylonjs/core/Lights/Shadows/shadowGeneratorSceneComponent";
import "@babylonjs/core/Layers/effectLayerSceneComponent"; // GlowLayer
import "@babylonjs/core/Materials/standardMaterial";
import "@babylonjs/core/Rendering/boundingBoxRenderer";

export { Engine } from "@babylonjs/core/Engines/engine";
export { Scene } from "@babylonjs/core/scene";
export { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
export { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
export { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
export { Plane } from "@babylonjs/core/Maths/math.plane";
export { DirectionalLight } from "@babylonjs/core/Lights/directionalLight";
export { HemisphericLight } from "@babylonjs/core/Lights/hemisphericLight";
export { PointLight } from "@babylonjs/core/Lights/pointLight";
export { ShadowGenerator } from "@babylonjs/core/Lights/Shadows/shadowGenerator";
export { GlowLayer } from "@babylonjs/core/Layers/glowLayer";
export { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
export { DynamicTexture } from "@babylonjs/core/Materials/Textures/dynamicTexture";
export { Texture } from "@babylonjs/core/Materials/Textures/texture";
export { Mesh } from "@babylonjs/core/Meshes/mesh";
export { MeshBuilder } from "@babylonjs/core/Meshes/meshBuilder";
export { TransformNode } from "@babylonjs/core/Meshes/transformNode";
export { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
export { SceneLoader } from "@babylonjs/core/Loading/sceneLoader";

export type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";

export type { PointerInfo } from "@babylonjs/core/Events/pointerEvents";
