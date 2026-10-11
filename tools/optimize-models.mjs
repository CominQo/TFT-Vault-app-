// Turns the raw champion GLBs (157 MB, 5-15 animations each) into small runtime models:
//  - keeps only the 5 animation roles the builder uses: idle, attack, run, dance, spell
//  - downsizes textures to 512px WebP, compresses geometry + animation with meshopt
// Usage: node optimize-models.mjs "<folder with *_(tft_set_18).glb>" [outDir]
import { NodeIO } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { dedup, prune, resample, textureCompress, meshopt, weld } from '@gltf-transform/functions';
import { MeshoptEncoder } from 'meshoptimizer';
import sharp from 'sharp';
import fs from 'fs';
import zlib from 'zlib';
import path from 'path';
import { pickClips, ROLES } from './anim-pick.mjs';


// Channels that hold a constant value equal to the bone's rest pose do nothing: remove them (cuts the JSON dramatically).
function stripStaticChannels(doc, eps = 1e-4) {
  let removed = 0, total = 0;
  for (const anim of doc.getRoot().listAnimations()) {
    for (const ch of anim.listChannels()) {
      total++;
      const s = ch.getSampler(), node = ch.getTargetNode(), path = ch.getTargetPath();
      if (!s || !node || s.getInterpolation() === 'CUBICSPLINE') continue;
      const out = s.getOutput(), arr = out.getArray(), k = out.getElementSize(), n = out.getCount();
      let same = true;
      for (let i = 1; i < n && same; i++) for (let j = 0; j < k; j++) if (Math.abs(arr[i * k + j] - arr[j]) > eps) { same = false; break; }
      if (!same) continue;
      const rest = path === 'translation' ? node.getTranslation() : path === 'rotation' ? node.getRotation() : path === 'scale' ? node.getScale() : null;
      if (!rest) continue;
      let eq = true;
      if (path === 'rotation') { const d = Math.abs(rest[0] * arr[0] + rest[1] * arr[1] + rest[2] * arr[2] + rest[3] * arr[3]); eq = d > 1 - 1e-6; }
      else for (let j = 0; j < 3; j++) if (Math.abs(rest[j] - arr[j]) > 1e-3 * Math.max(1, Math.abs(rest[j]))) eq = false;
      if (eq) { ch.dispose(); s.dispose(); removed++; }
    }
  }
  return [removed, total];
}
// Several meshes often carry identical copies of the same skeleton: point them all at the first one.
function mergeSkins(doc) {
  const skins = doc.getRoot().listSkins(); if (skins.length < 2) return 0;
  const keyOf = sk => sk.listJoints().map(j => doc.getRoot().listNodes().indexOf(j)).join(',');
  const first = new Map(); let merged = 0;
  for (const sk of skins) {
    const key = keyOf(sk);
    if (!first.has(key)) { first.set(key, sk); continue; }
    const keep = first.get(key);
    doc.getRoot().listNodes().forEach(nd => { if (nd.getSkin() === sk) nd.setSkin(keep); });
    sk.dispose(); merged++;
  }
  return merged;
}

// A champion arrives as ~10 separate mesh nodes sharing one skeleton. Quantizing clones the skeleton per mesh, so first fold
// every primitive into a single mesh (only when they share skin/parent and have an identity transform, otherwise leave as is).
function mergeMeshes(doc) {
  const nodes = doc.getRoot().listNodes().filter(n => n.getMesh());
  if (nodes.length < 2) return 0;
  const skin = nodes[0].getSkin(), parent = nodes[0].getParentNode();
  const ident = n => n.getMatrix().every((v, i) => Math.abs(v - (i % 5 === 0 ? 1 : 0)) < 1e-6);
  if (!nodes.every(n => n.getSkin() === skin && n.getParentNode() === parent && ident(n))) return 0;
  const target = nodes[0].getMesh();
  for (const n of nodes.slice(1)) {
    const m = n.getMesh();
    for (const prim of m.listPrimitives()) { m.removePrimitive(prim); target.addPrimitive(prim); }
    n.setMesh(null); n.setSkin(null);
  }
  return nodes.length - 1;
}

const src = process.argv[2] || '/tmp/champs/Champions - SET18';
const out = process.argv[3] || path.resolve('../frontend/models');   // shipped as .glb.gz (the GLB's JSON part gzips ~10x)
const rawOut = '/tmp/models_raw';                                      // uncompressed copies, for inspecting/debugging
fs.mkdirSync(out, { recursive: true }); fs.mkdirSync(rawOut, { recursive: true });
await MeshoptEncoder.ready;
const io = new NodeIO().registerExtensions(ALL_EXTENSIONS).registerDependencies({ 'meshopt.encoder': MeshoptEncoder });
const slug = f => f.split('_(')[0].toLowerCase().replace(/[^a-z0-9]+/g, '');
const manifest = {};
let before = 0, after = 0;

for (const f of fs.readdirSync(src).filter(x => x.endsWith('.glb')).sort()) {
  const doc = await io.read(path.join(src, f));
  const root = doc.getRoot();
  const clips = root.listAnimations().map(a => {
    let d = 0; a.listSamplers().forEach(s => { d = Math.max(d, s.getInput().getMax([])[0] || 0); });
    return { name: a.getName(), duration: d, anim: a };
  });
  const pick = pickClips(clips);
  const keepNames = new Set(ROLES.map(r => pick[r] && pick[r].name));
  const roleOf = {};                       // role -> final clip name ("idle", "attack", ...)
  const used = new Map();                  // original clip -> final name
  for (const r of ROLES) {
    const c = pick[r]; if (!c) continue;
    if (!used.has(c.name)) used.set(c.name, r);   // first role that claims a clip names it
    roleOf[r] = used.get(c.name);
  }
  for (const c of clips) {
    if (!keepNames.has(c.name)) c.anim.dispose();
    else c.anim.setName(used.get(c.name));
  }
  const merged = mergeMeshes(doc);
  await doc.transform(dedup(), prune(), weld(), resample({ tolerance: 0.002 }));
  const [removed, total] = stripStaticChannels(doc);
  await doc.transform(
    prune(), dedup(),
    textureCompress({ encoder: sharp, targetFormat: 'webp', resize: [512, 512], quality: 82 }),
    meshopt({ encoder: MeshoptEncoder, level: 'high' }),
  );
  const o = path.join(rawOut, slug(f) + '.glb');
  await io.write(o, doc);
  fs.writeFileSync(path.join(out, slug(f) + '.glb.gz'), zlib.gzipSync(fs.readFileSync(o), { level: 9 }));
  const a = fs.statSync(path.join(src, f)).size, b = fs.statSync(path.join(out, slug(f) + '.glb.gz')).size;
  before += a; after += b;
  manifest[slug(f)] = { roles: roleOf, clips: [...new Set(Object.values(roleOf))] };
  console.log(slug(f).padEnd(13), (a / 1e6).toFixed(1).padStart(5), 'MB ->', (b / 1e3).toFixed(0).padStart(5), 'KB | channels', total, '->', total - removed, '| meshes folded', merged);
}
fs.writeFileSync(path.join(out, 'manifest.json'), JSON.stringify(manifest));
console.log(`\nTOTAL ${(before / 1e6).toFixed(1)} MB -> ${(after / 1e6).toFixed(1)} MB  (${(100 * after / before).toFixed(0)}%)`);
