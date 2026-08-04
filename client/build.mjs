// Bundles the Babylon.js client into ../web (served by the Go backend).
// Tree-shaken @babylonjs/core keeps the payload a fraction of a Unity build.
import { build, context } from "esbuild";
import { cp, mkdir, readdir, readFile, stat, writeFile } from "node:fs/promises";
import { gzip } from "node:zlib";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import path from "node:path";

const gzipAsync = promisify(gzip);

const here = path.dirname(fileURLToPath(import.meta.url));
// Repo layout puts the bundle in ../web; the Docker client stage builds from
// /client, so its output lands in /web — same relative path either way.
const outDir = process.env.KR_OUT_DIR ?? path.join(here, "..", "web");
const watch = process.argv.includes("--watch");

// One self-contained bundle. Code splitting was tried and rejected: Babylon
// dynamically imports every shader, so esm+splitting produced ~200 files —
// far worse over HTTP/1.1 than a single gzipped request, for ~200 KB saved.
const options = {
  entryPoints: [path.join(here, "src", "main.ts")],
  bundle: true,
  format: "iife",
  target: ["es2020"],
  platform: "browser",
  outfile: path.join(outDir, "kingsreach.js"),
  minify: !watch,
  sourcemap: watch ? "inline" : false,
  legalComments: "none",
  logLevel: "info",
};
const mainBundle = options.outfile;

await mkdir(outDir, { recursive: true });
await cp(path.join(here, "static"), outDir, { recursive: true });

if (watch) {
  const ctx = await context(options);
  await ctx.watch();
  console.log("watching client sources -> ../web");
} else {
  await build(options);
  // Pre-compress the big static files; the Go server serves the .gz sibling to
  // clients that accept it, which is most of the payload win at no runtime cost.
  const mb = (n) => (n / 1024 / 1024).toFixed(2);
  const compress = async (file, label) => {
    await writeFile(file + ".gz", await gzipAsync(await readFile(file), { level: 9 }));
    const plain = (await stat(file)).size;
    const packed = (await stat(file + ".gz")).size;
    console.log(`${label} ${mb(plain)} MB -> ${mb(packed)} MB gzipped`);
  };
  await compress(mainBundle, "bundle");
  const modelDir = path.join(outDir, "models");
  for (const name of await readdir(modelDir).catch(() => [])) {
    if (name.endsWith(".glb")) await compress(path.join(modelDir, name), name);
  }
}
