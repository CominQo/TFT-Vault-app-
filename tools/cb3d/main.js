// 3D team-comp board for TFT Vault. Bundled by tools/build-js.mjs into frontend/lib/cb3d.js (global: CB3D).
import * as THREE from 'three';
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js';
import { MeshoptDecoder } from 'three/examples/jsm/libs/meshopt_decoder.module.js';
import { clone as cloneSkinned } from 'three/examples/jsm/utils/SkeletonUtils.js';

const COLS = 7, ROWS = 4, N = COLS * ROWS;
const R = 0.56, W = Math.sqrt(3) * R, DZ = 1.5 * R;          // hex circumradius / width / row spacing (pointy-top, odd rows shifted)
const cellXZ = i => { const r = Math.floor(i / COLS), k = i % COLS; return [k * W + (r % 2 ? W / 2 : 0) - 3.25 * W, (r - 1.5) * DZ]; };
const slugOf = n => String(n).toLowerCase().replace(/[^a-z0-9]+/g, '');
const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
const ease = { out: t => 1 - Math.pow(1 - t, 3), bounce: t => { const n = 7.5625, d = 2.75; if (t < 1 / d) return n * t * t; if (t < 2 / d) return n * (t -= 1.5 / d) * t + .75; if (t < 2.5 / d) return n * (t -= 2.25 / d) * t + .9375; return n * (t -= 2.625 / d) * t + .984375; } };

export function supported() {
  try { const c = document.createElement('canvas'); return !!(window.WebGLRenderingContext && (c.getContext('webgl2') || c.getContext('webgl'))) && typeof DecompressionStream === 'function'; }
  catch { return false; }
}


// Robust size/centre of a posed model: percentiles over the real (skinned) vertices, so staffs/wings/flames don't skew it.
const _v = new THREE.Vector3();
function modelStats(root) {
  root.updateMatrixWorld(true);
  const xs = [], ys = [], zs = [];
  root.traverse(o => {
    if (!(o.isMesh || o.isSkinnedMesh)) return;
    const pos = o.geometry.attributes.position; if (!pos) return;
    for (let i = 0; i < pos.count; i++) { if (o.isSkinnedMesh) o.getVertexPosition(i, _v); else _v.fromBufferAttribute(pos, i); _v.applyMatrix4(o.matrixWorld); xs.push(_v.x); ys.push(_v.y); zs.push(_v.z); }
  });
  const q = (a, f) => { a.sort((m, n) => m - n); return a[Math.min(a.length - 1, Math.max(0, Math.round(f * (a.length - 1))))]; };
  const st = { minY: q(ys, .002), maxY: q(ys, .99), cx: q(xs, .5), cz: q(zs, .5), x03: q(xs, .03), x97: q(xs, .97), z03: q(zs, .03), z97: q(zs, .97), yMid: 0 };
  st.h = Math.max(st.maxY - st.minY, 1e-3); st.w = Math.max(st.x97 - st.x03, 1e-3); st.d = Math.max(st.z97 - st.z03, 1e-3);
  return st;
}

// ---------- model loading (shared by the board and the portrait renderer) ----------
const loader = new GLTFLoader().setMeshoptDecoder(MeshoptDecoder);
const models = new Map();          // slug -> Promise<model>
let manifestP = null;
const manifest = base => manifestP || (manifestP = fetch(base + 'manifest.json').then(r => r.json()).catch(() => ({})));

async function fetchGz(url) {
  const r = await fetch(url);
  if (!r.ok) throw new Error('model not found: ' + url);
  return new Response(r.body.pipeThrough(new DecompressionStream('gzip'))).arrayBuffer();
}
export function loadModel(base, name) {
  const slug = slugOf(name);
  if (!models.has(slug)) {
    models.set(slug, (async () => {
      const [buf, mf] = await Promise.all([fetchGz(base + slug + '.glb.gz'), manifest(base)]);
      const gltf = await loader.parseAsync(buf, '');
      const scene = gltf.scene;
      scene.traverse(o => { if (o.isMesh || o.isSkinnedMesh) { o.frustumCulled = false; o.castShadow = false; } });
      scene.updateMatrixWorld(true);
      const st = modelStats(scene), size = new THREE.Vector3(st.w, st.h, st.d);
      const roles = (mf[slug] && mf[slug].roles) || {};
      const clip = r => THREE.AnimationClip.findByName(gltf.animations, roles[r] || r);
      return { slug, scene, st, size, clips: { idle: clip('idle'), attack: clip('attack'), run: clip('run'), dance: clip('dance'), spell: clip('spell') } };
    })().catch(e => { models.delete(slug); throw e; }));
  }
  return models.get(slug);
}
const BIG = { elderdragon: { cap: 2.1, boost: 1.35 }, maokai: { cap: 1.6, boost: 1.1 } };      // deliberately larger than a hex
function normScale(slug, size) {
  const o = BIG[slug] || {}, cap = o.cap || 1.25, h = Math.max(size.y, 1e-3), foot = Math.max(size.x, size.z, 1e-3);
  const target = clamp(1.15 * Math.pow(h / 2.0, .42), .72, 1.45) * (o.boost || 1);       // height keeps relative sizes without extremes...
  return Math.min(target / h, cap / foot);                                                // ...and wide creatures are capped by footprint
}

// ---------- the board ----------
export function create(container, opts = {}) {
  const base = opts.modelsUrl || 'models/';
  const costColor = opts.costColor || (() => '#f8a900');
  const GOLD = new THREE.Color('#f8a900');

  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' });
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.75));
  renderer.setClearColor(0x000000, 0);
  const canvas = renderer.domElement; canvas.className = 'cb3-canvas';
  container.appendChild(canvas);
  const scene = new THREE.Scene();
  const camera = new THREE.PerspectiveCamera(26, 1, 0.1, 100);
  const cam = { az: 0, el: 0.95, zoom: 1, tAz: 0, tEl: 0.95, tZoom: 1, fit: 14 };
  const target = new THREE.Vector3(0, 0.45, 0.15);

  // lights (only the tiles/platform are lit; champions use unlit materials)
  scene.add(new THREE.HemisphereLight(0xcfd8ff, 0x1a1f2b, 1.1));
  const key = new THREE.DirectionalLight(0xffe2b0, 1.4); key.position.set(-3, 7, 5); scene.add(key);
  const rim = new THREE.PointLight(0x6aa8ff, 18, 14); rim.position.set(5, 2.5, -4); scene.add(rim);

  // platform + soft glow
  const glowTex = (() => { const c = document.createElement('canvas'); c.width = c.height = 256; const g = c.getContext('2d'), gr = g.createRadialGradient(128, 128, 0, 128, 128, 128);
    gr.addColorStop(0, 'rgba(255,255,255,.95)'); gr.addColorStop(.55, 'rgba(255,255,255,.28)'); gr.addColorStop(1, 'rgba(255,255,255,0)'); g.fillStyle = gr; g.fillRect(0, 0, 256, 256); return new THREE.CanvasTexture(c); })();
  const plateShape = (() => { const w = 7.5 * W / 2 + .42, d = 1.5 * DZ + R + .32, rr = .5, sh = new THREE.Shape();
    sh.moveTo(-w + rr, -d); sh.lineTo(w - rr, -d); sh.quadraticCurveTo(w, -d, w, -d + rr); sh.lineTo(w, d - rr); sh.quadraticCurveTo(w, d, w - rr, d);
    sh.lineTo(-w + rr, d); sh.quadraticCurveTo(-w, d, -w, d - rr); sh.lineTo(-w, -d + rr); sh.quadraticCurveTo(-w, -d, -w + rr, -d); return sh; })();
  const plateGeo = new THREE.ExtrudeGeometry(plateShape, { depth: .18, bevelEnabled: true, bevelThickness: .03, bevelSize: .03, bevelSegments: 2 }); plateGeo.rotateX(-Math.PI / 2);
  const platform = new THREE.Mesh(plateGeo, new THREE.MeshStandardMaterial({ color: 0x0d121b, roughness: .6, metalness: .4 }));
  platform.position.y = -0.23; scene.add(platform);
  const edge = new THREE.Line(new THREE.BufferGeometry().setFromPoints(plateShape.getPoints(24).map(p => new THREE.Vector3(p.x, 0, -p.y))), new THREE.LineBasicMaterial({ color: GOLD, transparent: true, opacity: .5, blending: THREE.AdditiveBlending }));
  edge.position.y = -0.03; scene.add(edge);
  const aura = new THREE.Mesh(new THREE.PlaneGeometry(17, 12), new THREE.MeshBasicMaterial({ map: glowTex, color: 0xf8a900, transparent: true, opacity: .14, blending: THREE.AdditiveBlending, depthWrite: false }));
  aura.rotation.x = -Math.PI / 2; aura.position.y = -0.24; scene.add(aura);

  // floating motes
  const motes = []; { const g = new THREE.CircleGeometry(.035, 6);
    for (let i = 0; i < 26; i++) { const m = new THREE.Mesh(g, new THREE.MeshBasicMaterial({ color: i % 3 ? 0xf8a900 : 0x6aa8ff, transparent: true, opacity: .0, depthWrite: false, blending: THREE.AdditiveBlending }));
      m.userData = { x: (Math.random() - .5) * 11, z: (Math.random() - .5) * 8, y: Math.random() * 3, v: .12 + Math.random() * .22, ph: Math.random() * 6.28 }; m.rotation.x = -Math.PI / 2; scene.add(m); motes.push(m); } }

  // tiles
  const hexShape = (() => { const s = new THREE.Shape(), r = R * .965; for (let k = 0; k < 6; k++) { const a = Math.PI / 2 + k * Math.PI / 3; k ? s.lineTo(Math.cos(a) * r, Math.sin(a) * r) : s.moveTo(Math.cos(a) * r, Math.sin(a) * r); } s.closePath(); return s; })();
  const tileGeo = new THREE.ExtrudeGeometry(hexShape, { depth: .11, bevelEnabled: true, bevelThickness: .022, bevelSize: .022, bevelSegments: 2 }); tileGeo.rotateX(-Math.PI / 2);
  const rimGeo = new THREE.RingGeometry(R * .84, R * .9, 6, 1, Math.PI / 2); rimGeo.rotateX(-Math.PI / 2);
  const ringGeo = new THREE.RingGeometry(.33, .43, 48); ringGeo.rotateX(-Math.PI / 2);
  const discGeo = new THREE.CircleGeometry(.46, 40); discGeo.rotateX(-Math.PI / 2);
  const tiles = [], tileMeshes = [];
  for (let i = 0; i < N; i++) {
    const [x, z] = cellXZ(i), r = Math.floor(i / COLS), k = i % COLS;
    const base = new THREE.Color((r + k) % 2 ? '#1c2434' : '#222b3c');
    const mesh = new THREE.Mesh(tileGeo, new THREE.MeshStandardMaterial({ color: base, roughness: .55, metalness: .3, emissive: 0x000000 }));
    mesh.position.set(x, 0, z); mesh.userData.i = i; scene.add(mesh); tileMeshes.push(mesh);
    const rimM = new THREE.Mesh(rimGeo, new THREE.MeshBasicMaterial({ color: GOLD, transparent: true, opacity: .16, blending: THREE.AdditiveBlending, depthWrite: false }));
    rimM.position.set(x, .136, z); scene.add(rimM);
    tiles.push({ mesh, rim: rimM, base, lift: 0, glow: 0 });
  }

  // ---------- units ----------
  const byName = new Map(), at = new Array(N).fill(null);
  const fxs = [];
  let hoverCell = -1, dropCell = -1, hoverUnit = null, dragging = null, celebrate = false, running = false, raf = 0, last = 0, clock = 0, nextEmote = 6;
  const shadowMat = new THREE.MeshBasicMaterial({ map: glowTex, color: 0x000000, transparent: true, opacity: .55, depthWrite: false });
  const shadowGeo = new THREE.PlaneGeometry(.95, .95); shadowGeo.rotateX(-Math.PI / 2);

  function fxRing(x, z, color, size = 1) {
    const m = new THREE.Mesh(new THREE.RingGeometry(.2, .26, 40), new THREE.MeshBasicMaterial({ color, transparent: true, opacity: .9, blending: THREE.AdditiveBlending, depthWrite: false, side: THREE.DoubleSide }));
    m.rotation.x = -Math.PI / 2; m.position.set(x, .16, z); scene.add(m);
    fxs.push({ t: 0, dur: .6, update(p) { const s = 1 + p * 3.2 * size; m.scale.set(s, s, s); m.material.opacity = .9 * (1 - p); }, end() { scene.remove(m); m.geometry.dispose(); m.material.dispose(); } });
  }

  function makeUnit(name, i, info) {
    const u = { name, i, info, group: new THREE.Group(), visual: new THREE.Group(), model: null, mixer: null, actions: {}, cur: '', busy: 0,
      pos: new THREE.Vector3(), goal: new THREE.Vector3(), yaw: 0, spawn: 0, dying: 0, h: 1.2, color: new THREE.Color(costColor(info.cost)) };
    const [x, z] = cellXZ(i); u.pos.set(x, .13, z); u.goal.copy(u.pos);
    u.group.position.copy(u.pos); u.group.add(u.visual); scene.add(u.group);
    // ground decoration: soft shadow + cost ring
    u.shadow = new THREE.Mesh(shadowGeo, shadowMat); u.shadow.position.y = .02; u.group.add(u.shadow);
    u.disc = new THREE.Mesh(discGeo, new THREE.MeshBasicMaterial({ color: u.color, transparent: true, opacity: .16, blending: THREE.AdditiveBlending, depthWrite: false })); u.disc.position.y = .015; u.group.add(u.disc);
    u.ring = new THREE.Mesh(ringGeo, new THREE.MeshBasicMaterial({ color: u.color, transparent: true, opacity: .85, blending: THREE.AdditiveBlending, depthWrite: false })); u.ring.position.y = .022; u.group.add(u.ring);
    // click/hover proxy
    u.hit = new THREE.Mesh(new THREE.CylinderGeometry(.34, .34, 1, 10), new THREE.MeshBasicMaterial({ visible: false })); u.hit.position.y = .6; u.hit.userData.unit = u; u.group.add(u.hit);
    // placeholder until the model arrives: spinning glow
    u.ph = new THREE.Mesh(new THREE.CylinderGeometry(.17, .17, .05, 6), new THREE.MeshBasicMaterial({ color: u.color, transparent: true, opacity: .8, blending: THREE.AdditiveBlending })); u.ph.position.y = .5; u.visual.add(u.ph);
    u.spawn = .0001;
    loadModel(base, name).then(m => attach(u, m)).catch(() => { u.failed = true; u.ph.material.color.set(0x884444); });
    return u;
  }
  function attach(u, m) {
    if (u.dead) return;
    const root = cloneSkinned(m.scene), s = normScale(m.slug, m.size);
    root.scale.setScalar(s);
    // stand on the floor, centred on the hex (bind-pose bounding box)
    root.position.set(-m.st.cx * s, -m.st.minY * s, -m.st.cz * s);
    u.visual.add(root); u.visual.remove(u.ph); u.ph.geometry.dispose(); u.ph.material.dispose(); u.ph = null;
    u.model = root; u.h = m.size.y * s; u.hit.scale.y = Math.max(.5, u.h); u.hit.position.y = u.h / 2; u.hit.scale.x = u.hit.scale.z = clamp(m.size.x * s / .7, .8, 1.5);
    u.mixer = new THREE.AnimationMixer(root);
    u.mixer.addEventListener('finished', () => { u.busy = 0; if (!celebrate) play(u, 'idle'); else play(u, 'dance'); });
    for (const r of ['idle', 'attack', 'run', 'dance', 'spell']) { const c = m.clips[r]; if (c) u.actions[r] = u.mixer.clipAction(c); }
    if (u.actions.idle) u.actions.idle.time = Math.random() * u.actions.idle.getClip().duration;
    play(u, celebrate ? 'dance' : 'idle', 0);
    u.ready = true; u.loadedAt = clock;
  }
  function play(u, role, fade = .22, once = false) {
    const a = u.actions[role] || u.actions.idle; if (!a) return;
    if (u.cur === role && !once) return;
    const prev = u.actions[u.cur];
    a.reset(); a.enabled = true; a.setEffectiveWeight(1); a.setEffectiveTimeScale(1);
    a.setLoop(once ? THREE.LoopOnce : THREE.LoopRepeat, Infinity); a.clampWhenFinished = once;
    if (prev && prev !== a) a.crossFadeFrom(prev, fade, false);
    a.play(); u.cur = role; u.busy = once ? 1 : 0;
  }
  const emote = (u, role, force) => { if (!u.ready || u.dying || dragging || (celebrate && !force)) return; play(u, u.actions[role] ? role : 'attack', .15, true); };   // no random flourishes while everyone is celebrating
  const pending = [];                         // removed units that are still playing their poof
  function removeUnit(u) { u.dying = .0001; u.dead = true; pending.push(u); }

  function setBoard(names, infoOf) {
    const want = new Map(); names.forEach((n, i) => { if (n) want.set(n, i); });
    for (const [n, u] of [...byName]) if (!want.has(n)) { removeUnit(u); byName.delete(n); }
    at.fill(null);
    for (const [n, i] of want) {
      let u = byName.get(n);
      if (!u) { u = makeUnit(n, i, infoOf(n) || { cost: 1 }); byName.set(n, u); }
      else if (u.i !== i && !(dragging && dragging.unit === u)) { u.i = i; const [x, z] = cellXZ(i); u.goal.set(x, .13, z); }
      else if (dragging && dragging.unit === u) { u.i = i; const [x, z] = cellXZ(i); u.goal.set(x, .13, z); }
      at[i] = u;
    }
  }

  // ---------- picking ----------
  const ray = new THREE.Raycaster(), ndc = new THREE.Vector2(), plane = new THREE.Plane(new THREE.Vector3(0, 1, 0), -.13), hitP = new THREE.Vector3();
  const setNdc = (cx, cy) => { const r = canvas.getBoundingClientRect(); ndc.set(((cx - r.left) / r.width) * 2 - 1, -((cy - r.top) / r.height) * 2 + 1); ray.setFromCamera(ndc, camera); };
  function groundPoint(cx, cy) { setNdc(cx, cy); return ray.ray.intersectPlane(plane, hitP) ? hitP : null; }
  function pickCell(cx, cy, tol = .93) {
    const p = groundPoint(cx, cy); if (!p) return -1;
    let best = -1, bd = R * tol;
    for (let i = 0; i < N; i++) { const [x, z] = cellXZ(i), d = Math.hypot(p.x - x, p.z - z); if (d < bd) { bd = d; best = i; } }
    return best;
  }
  function pickUnit(cx, cy) {
    setNdc(cx, cy);
    const hits = ray.intersectObjects([...byName.values()].filter(u => !u.dying).map(u => u.hit), false);
    return hits.length ? hits[0].object.userData.unit : null;
  }

  // ---------- camera ----------
  function layout() {
    const w = container.clientWidth || 1, h = container.clientHeight || 1;
    renderer.setSize(w, h, false); canvas.style.width = '100%'; canvas.style.height = '100%';
    camera.aspect = w / h; camera.updateProjectionMatrix();
    const halfW = 4.0, vf = THREE.MathUtils.degToRad(camera.fov / 2), hf = Math.atan(Math.tan(vf) * camera.aspect);
    cam.fit = Math.max(halfW / Math.tan(hf), 2.65 / Math.tan(vf)) * 1.03;
  }
  function placeCamera() {
    const d = cam.fit * cam.zoom, ce = Math.cos(cam.el), se = Math.sin(cam.el);
    camera.position.set(target.x + d * ce * Math.sin(cam.az), target.y + d * se, target.z + d * ce * Math.cos(cam.az));
    camera.lookAt(target);
  }

  // ---------- input ----------
  let down = null;
  canvas.addEventListener('contextmenu', e => e.preventDefault());
  canvas.addEventListener('pointerdown', e => {
    canvas.setPointerCapture(e.pointerId);
    const u = e.button === 0 ? pickUnit(e.clientX, e.clientY) : null;
    down = { x: e.clientX, y: e.clientY, lx: e.clientX, ly: e.clientY, unit: u, moved: false };
  });
  canvas.addEventListener('pointermove', e => {
    if (!down) {
      const u = pickUnit(e.clientX, e.clientY), c = pickCell(e.clientX, e.clientY);
      if (u !== hoverUnit) { hoverUnit = u; opts.onHoverUnit && opts.onHoverUnit(u ? u.i : -1); }
      if (c !== hoverCell) { hoverCell = c; opts.onHoverCell && opts.onHoverCell(c); }
      canvas.style.cursor = u ? 'grab' : 'default';
      return;
    }
    if (!down.moved && Math.hypot(e.clientX - down.x, e.clientY - down.y) > 5) {
      down.moved = true;
      if (down.unit && !down.unit.dying) { dragging = { unit: down.unit, from: down.unit.i }; canvas.style.cursor = 'grabbing'; opts.onDragStart && opts.onDragStart(down.unit.i); }
    }
    if (!down.moved) return;
    if (dragging) {
      const p = groundPoint(e.clientX, e.clientY), u = dragging.unit;
      if (p) { u.goal.set(clamp(p.x, -5.2, 5.2), .13 + .45, clamp(p.z, -3.4, 3.4)); }
      hoverCell = pickCell(e.clientX, e.clientY, 1.15); opts.onDragHover && opts.onDragHover(hoverCell);
    } else {   // orbit
      cam.tAz = clamp(cam.tAz - (e.clientX - down.lx) * .006, -.75, .75);
      cam.tEl = clamp(cam.tEl + (e.clientY - down.ly) * .005, .5, 1.28);
    }
    down.lx = e.clientX; down.ly = e.clientY;
  });
  const finish = e => {
    if (!down) return;
    const d = down; down = null;
    try { canvas.releasePointerCapture(e.pointerId); } catch {}
    if (dragging) {
      const u = dragging.unit, from = dragging.from, to = pickCell(e.clientX, e.clientY, 1.15); dragging = null; canvas.style.cursor = 'default';
      const [x, z] = cellXZ(from); u.goal.set(x, .13, z);                       // default: glide back; setBoard() re-targets if the move is accepted
      opts.onDragEnd && opts.onDragEnd(from, to);
    } else if (d.unit && !d.moved && e.type === 'pointerup') {
      emote(d.unit, Math.random() < .5 ? 'attack' : 'spell', true); fxRing(d.unit.pos.x, d.unit.pos.z, d.unit.color, .6);
      opts.onUnitClick && opts.onUnitClick(d.unit.i);
    }
  };
  canvas.addEventListener('pointerup', finish); canvas.addEventListener('pointercancel', finish);
  canvas.addEventListener('pointerleave', () => { if (!down) { hoverCell = -1; if (hoverUnit) { hoverUnit = null; opts.onHoverUnit && opts.onHoverUnit(-1); } opts.onHoverCell && opts.onHoverCell(-1); } });
  canvas.addEventListener('wheel', e => { e.preventDefault(); cam.tZoom = clamp(cam.tZoom * (1 + Math.sign(e.deltaY) * .08), .62, 1.25); }, { passive: false });
  canvas.addEventListener('dblclick', () => resetView());
  function resetView() { cam.tAz = 0; cam.tEl = .95; cam.tZoom = 1; }

  // ---------- frame loop ----------
  const tmp = new THREE.Vector3();
  function frame(now) {
    raf = running ? requestAnimationFrame(frame) : 0;
    const dt = Math.min(.05, (now - last) / 1000 || .016); last = now; clock += dt;
    cam.az += (cam.tAz - cam.az) * Math.min(1, dt * 8); cam.el += (cam.tEl - cam.el) * Math.min(1, dt * 8); cam.zoom += (cam.tZoom - cam.zoom) * Math.min(1, dt * 8);
    placeCamera();
    // tiles
    for (let i = 0; i < N; i++) {
      const t = tiles[i], occupied = !!at[i] && !at[i].dying, hot = i === dropCell ? 1 : (i === hoverCell ? (dragging ? 1 : .55) : 0);
      t.glow += (hot - t.glow) * Math.min(1, dt * 14); t.lift += ((hot ? .045 : 0) - t.lift) * Math.min(1, dt * 14);
      t.mesh.position.y = t.lift;
      t.mesh.material.color.copy(t.base).lerp(GOLD, t.glow * .45);
      t.mesh.material.emissive.copy(GOLD).multiplyScalar(t.glow * .35);
      t.rim.material.opacity = .14 + t.glow * .6 + (occupied ? .14 : 0);
      t.rim.material.color.copy(occupied ? at[i].color : GOLD); t.rim.position.y = .136 + t.lift;
    }
    // units
    for (const u of [...byName.values(), ...pending]) {
      if (u.spawn > 0 && !u.dying) {                                        // drop-in
        u.spawn += dt; const p = clamp(u.spawn / .75, 0, 1);
        u.visual.position.y = (1 - ease.bounce(p)) * 2.6; u.visual.scale.setScalar(.3 + .7 * ease.out(clamp(p * 1.6, 0, 1)));
        if (p >= 1) { u.spawn = 0; u.visual.position.y = 0; u.visual.scale.setScalar(1); fxRing(u.pos.x, u.pos.z, u.color); if (u.ready) emote(u, 'attack'); }
      }
      if (u.dying) {                                                        // poof
        u.dying += dt; const p = clamp(u.dying / .28, 0, 1); u.visual.scale.setScalar(1 - ease.out(p)); u.visual.position.y = p * .4;
        u.ring.material.opacity = .85 * (1 - p); u.disc.material.opacity = .16 * (1 - p);
        if (!u.poofed) { u.poofed = true; fxRing(u.pos.x, u.pos.z, 0xffffff, .5); }
        if (p >= 1) { scene.remove(u.group); [u.ring, u.disc, u.hit, u.ph].forEach(o => { if (o) { o.geometry !== ringGeo && o.geometry !== discGeo && o.geometry.dispose(); o.material.dispose(); } }); u.gone = true; }
        continue;
      }
      // movement towards the goal (drag follows pointer; otherwise glide to the hex with the run animation)
      tmp.copy(u.goal).sub(u.pos); const dist = Math.hypot(tmp.x, tmp.z);
      if (dragging && dragging.unit === u) { u.pos.lerp(u.goal, Math.min(1, dt * 18)); u.group.position.copy(u.pos); }
      else {
        if (dist > .02) {
          const sp = Math.min(dist, 7.5 * dt * (dist > 1 ? 1.4 : 1) + dist * dt * 4); u.pos.x += tmp.x / dist * sp; u.pos.z += tmp.z / dist * sp; u.pos.y += (u.goal.y - u.pos.y) * Math.min(1, dt * 12);
          const want = Math.atan2(tmp.x, tmp.z); let dy = want - u.yaw; dy = Math.atan2(Math.sin(dy), Math.cos(dy)); u.yaw += dy * Math.min(1, dt * 14);
          if (u.ready && u.cur !== 'run' && dist > .12) play(u, 'run', .12);
        } else {
          u.pos.copy(u.goal);
          if (u.ready && u.cur === 'run') play(u, celebrate ? 'dance' : 'idle', .2);
          u.yaw += (-u.yaw) * Math.min(1, dt * 10);
        }
        u.group.position.copy(u.pos);
      }
      u.visual.rotation.y = u.yaw;
      const lift = dragging && dragging.unit === u ? 1 : 0;
      u.shadow.position.y = .02 - u.group.position.y + .15; u.shadow.scale.setScalar(1 - lift * .15);
      u.ring.visible = u.disc.visible = true;
      const pulse = .5 + .5 * Math.sin(clock * 2.4 + u.i); u.ring.material.opacity = .55 + .35 * pulse + (u === hoverUnit ? .2 : 0); u.disc.material.opacity = .14 + .08 * pulse;
      u.ring.position.y = .022 - u.group.position.y + .15; u.disc.position.y = .015 - u.group.position.y + .15;
      if (u.mixer) u.mixer.update(dt);
    }
    for (let k = pending.length - 1; k >= 0; k--) if (pending[k].gone) pending.splice(k, 1);
    // idle emotes: now and then a champion shows off
    if ((nextEmote -= dt) < 0) { nextEmote = 5 + Math.random() * 7; const c = [...byName.values()].filter(u => u.ready && u.cur === 'idle' && !u.dying); if (c.length && !celebrate) emote(c[Math.floor(Math.random() * c.length)], Math.random() < .6 ? 'attack' : 'spell'); }
    // fx + ambience
    for (let k = fxs.length - 1; k >= 0; k--) { const f = fxs[k]; f.t += dt; const p = clamp(f.t / f.dur, 0, 1); f.update(p); if (p >= 1) { f.end(); fxs.splice(k, 1); } }
    for (const m of motes) { const d = m.userData; d.y += d.v * dt; if (d.y > 3.2) { d.y = 0; d.x = (Math.random() - .5) * 11; d.z = (Math.random() - .5) * 8; } m.position.set(d.x + Math.sin(clock * .6 + d.ph) * .2, d.y, d.z); m.material.opacity = Math.sin(clamp(d.y / 3.2, 0, 1) * Math.PI) * .5; }
    edge.material.opacity = .38 + .16 * Math.sin(clock * 1.2);
    renderer.render(scene, camera);
    opts.onFrame && opts.onFrame();
  }
  // ---------- public API ----------
  const api = {
    setBoard, pick: pickCell, pickUnit, resetView,
    setDropHover(i) { dropCell = i; },
    celebrate(on) { celebrate = !!on; for (const u of byName.values()) if (u.ready && !u.dying) play(u, celebrate ? 'dance' : 'idle', .3); },
    isCelebrating: () => celebrate,
    emoteAll(role = 'attack') { let k = 0; for (const u of byName.values()) setTimeout(() => emote(u, role, true), k++ * 90); },
    screenOf(i) {                                    // container-relative pixel position of the unit's feet and head (for DOM overlays)
      const u = at[i]; if (!u) return null;
      const w = canvas.clientWidth, h = canvas.clientHeight, f = new THREE.Vector3(u.pos.x, u.pos.y, u.pos.z).project(camera), t = new THREE.Vector3(u.pos.x, u.pos.y + u.h + .12, u.pos.z).project(camera);
      return { x: (f.x * .5 + .5) * w, y: (-f.y * .5 + .5) * h, topX: (t.x * .5 + .5) * w, topY: (-t.y * .5 + .5) * h };
    },
    cellScreen(i) {                                   // container-relative pixel position of a cell centre (used for hit-testing / tests)
      const [x, z] = cellXZ(i), v = new THREE.Vector3(x, .13, z).project(camera);
      return { x: (v.x * .5 + .5) * canvas.clientWidth, y: (-v.y * .5 + .5) * canvas.clientHeight };
    },
    start() { if (running) return; running = true; layout(); last = performance.now(); raf = requestAnimationFrame(frame); },
    stop() { running = false; if (raf) cancelAnimationFrame(raf); raf = 0; },
    resize: layout,
    isReady: () => [...byName.values()].every(u => u.ready || u.failed),
    state: () => ({ units: [...byName.values()].map(u => ({ name: u.name, i: u.i, ready: !!u.ready, anim: u.cur, h: +u.h.toFixed(2) })), cam: { ...cam } }),
    dispose() { api.stop(); ro.disconnect(); renderer.dispose(); canvas.remove(); },
  };
  const ro = new ResizeObserver(() => { layout(); }); ro.observe(container);
  layout(); placeCamera();
  return api;
}

// ---------- portraits (used at build time to render the champion icons; also handy for the UI) ----------
export async function renderPortraits(base, names, size = 160, onEach) {
  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, preserveDrawingBuffer: true });
  renderer.setSize(size, size, false); renderer.setPixelRatio(1); renderer.setClearColor(0x000000, 0);
  const scene = new THREE.Scene(), cam = new THREE.OrthographicCamera(-1, 1, 1, -1, .01, 200);
  const out = {};
  for (const name of names) {
    try {
      const m = await loadModel(base, name), root = cloneSkinned(m.scene);
      const mixer = new THREE.AnimationMixer(root); if (m.clips.idle) { const a = mixer.clipAction(m.clips.idle); a.play(); mixer.update(0.001); }
      scene.add(root); const st = modelStats(root);
      const wide = st.w > st.h * .9, frameH = wide ? Math.max(st.w * 1.12, st.h * 1.06) : st.h * .6;
      const cy = wide ? (st.minY + st.maxY) / 2 : st.maxY - frameH / 2 + st.h * .015, half = frameH / 2;
      cam.left = -half; cam.right = half; cam.top = half; cam.bottom = -half; cam.updateProjectionMatrix();
      cam.position.set(st.cx, cy, 80); cam.lookAt(st.cx, cy, 0);
      renderer.render(scene, cam);
      out[slugOf(name)] = renderer.domElement.toDataURL('image/png');
      scene.remove(root); onEach && onEach(name);
    } catch (e) { out[slugOf(name)] = null; }
  }
  renderer.dispose();
  return out;
}
export { slugOf, cellXZ };
