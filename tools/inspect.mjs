import { NodeIO } from '@gltf-transform/core';
import { ALL_EXTENSIONS } from '@gltf-transform/extensions';
import { MeshoptDecoder } from 'meshoptimizer';
await MeshoptDecoder.ready;
const io = new NodeIO().registerExtensions(ALL_EXTENSIONS).registerDependencies({ 'meshopt.decoder': MeshoptDecoder });
const f = process.argv[2];
const doc = await io.read(f);
const root = doc.getRoot();
const bytes = a => a.getArray() ? a.getArray().byteLength : 0;
let cat = { meshAttr: 0, indices: 0, ibm: 0, animIn: 0, animOut: 0, other: 0 };
const seen = new Set();
const add = (a, k) => { if (!a || seen.has(a)) return; seen.add(a); cat[k] += bytes(a); };
for (const m of root.listMeshes()) for (const p of m.listPrimitives()) {
  p.listAttributes().forEach(a => add(a, 'meshAttr')); add(p.getIndices(), 'indices');
}
for (const s of root.listSkins()) add(s.getInverseBindMatrices(), 'ibm');
for (const an of root.listAnimations()) for (const s of an.listSamplers()) { add(s.getInput(), 'animIn'); add(s.getOutput(), 'animOut'); }
console.log('file bytes on disk:', (await import('fs')).statSync(f).size);
console.log('DECODED sizes (bytes):', cat);
console.log('meshes', root.listMeshes().length, 'prims', root.listMeshes().reduce((a, m) => a + m.listPrimitives().length, 0), '| nodes', root.listNodes().length, '| skins', root.listSkins().length, '| joints', root.listSkins().map(s => s.listJoints().length), '| anims', root.listAnimations().map(a => a.getName() + ':' + a.listChannels().length + 'ch'), '| textures', root.listTextures().map(t => (t.getSize() || []).join('x') + ' ' + t.getMimeType()));
