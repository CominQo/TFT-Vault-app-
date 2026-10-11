"""Re-applies the 3D board integration to frontend/index.html (idempotent: does nothing if already applied).
Written as string replacements so it fails loudly if the surrounding code changed."""
import sys
p = sys.argv[1] if len(sys.argv) > 1 else 'frontend/index.html'
h = open(p, encoding='utf-8').read()
if 'cb-3d' in h and 'mount3d' in h:
    print('already applied'); sys.exit(0)
def sub(h, old, new, count=1):
    assert h.count(old) >= 1, "MISSING: " + old[:110]
    return h.replace(old, new, count)

# 1. bundle
first = h.index('<script>'); h = h[:first] + '<script src="lib/cb3d.js"></script>\n' + h[first:]
# 2. state + icons rendered from the 3D models
h = sub(h, "  const img = c => c.icon || c.img || c.image || c.portrait || '';",
"""  // 3D board: needs WebGL; otherwise the classic 2D board below keeps working
  const OK3D = !!(window.CB3D && CB3D.supported());
  let B3 = null, MF3 = null, tagIdx = -1, tagTimer = 0, tagHold = false;
  const slug3 = n => (window.CB3D ? CB3D.slugOf(n) : '');
  const portrait = c => (MF3 && MF3[slug3(c.name)] ? 'models/portraits/' + slug3(c.name) + '.webp' : '');   // icons rendered from the 3D models
  const img = c => c.icon || c.img || c.image || c.portrait || portrait(c);""")
# 3. board markup
h = sub(h, '<div class="cb-panel cb-board"><div id="cb-bd"></div><div class="cb-rm" id="cb-rm">Drop here to remove</div></div>${recShell()}',
'''<div class="cb-panel cb-board${OK3D ? ' is3d' : ''}">${OK3D ? `<div class="cb-3d" id="cb-3d"><div class="cb3-ov" id="cb3-ov"><div class="cb3-tag" id="cb3-tag" hidden></div></div>
          <div class="cb3-ctl"><button class="cb3-b" data-cel title="Make everyone dance"><svg viewBox="0 0 24 24"><path d="M9 18V6l11-2v12M9 18a3 3 0 11-3-3 3 3 0 013 3zm11-2a3 3 0 11-3-3 3 3 0 013 3z"/></svg>Celebrate</button><button class="cb3-b" data-rv title="Reset camera (or double-click the board)"><svg viewBox="0 0 24 24"><path d="M3 12a9 9 0 109-9M3 4v5h5"/></svg></button></div>
          <div class="cb3-hint">Drag champions onto the board · drag a champion to move it · drag the background to rotate · scroll to zoom</div></div>` : '<div id="cb-bd"></div>'}<div class="cb-rm" id="cb-rm">Drop here to remove</div></div>${recShell()}''')
h = sub(h, "    $('cb-bd').innerHTML = [0,1,2,3].map(", "    if (OK3D) mount3d(); else $('cb-bd').innerHTML = [0,1,2,3].map(")
# 4. paintBoard
h = sub(h, "    document.querySelectorAll('.cb-hex').forEach(h => {\n      const i = +h.dataset.i, c = board[i] && champ(board[i]);", "    if (B3) sync3d();\n    else document.querySelectorAll('.cb-hex').forEach(h => {\n      const i = +h.dataset.i, c = board[i] && champ(board[i]);")
# 5. picking + the 3D glue
h = sub(h, "  const clearOver = () => root.querySelectorAll('.over').forEach(x => x.classList.remove('over'));",
"""  const clearOver = () => { root.querySelectorAll('.over').forEach(x => x.classList.remove('over')); if (B3) B3.setDropHover(-1); };

  /* ---------- 3D board ---------- */
  // which board cell is under the pointer (3D: ray-cast onto the board; 2D: the hex element). -1 = none
  const hexOf = e => {
    if (B3) { const el = e.target.closest && e.target.closest('#cb-3d'); return el ? B3.pick(e.clientX, e.clientY) : -1; }
    const hx = e.target.closest('.cb-hex'); return hx ? +hx.dataset.i : -1;
  };
  function mount3d() {
    const el = $('cb-3d');
    B3 = CB3D.create(el, {
      modelsUrl: 'models/', costColor: c => COST[c], onFrame: frame3d,
      onHoverUnit: i => { clearTimeout(tagTimer); if (i >= 0) { tagIdx = i; paintTag(); } else tagTimer = setTimeout(() => { if (!tagHold) { tagIdx = -1; paintTag(); } }, 280); },
      onUnitClick: i => { tagIdx = i; paintTag(); },
      onDragStart: () => { root.dataset.drag = 'unit'; tagIdx = -1; paintTag(); },
      onDragHover: i => $('cb-rm').classList.toggle('over', i < 0),
      onDragEnd: (from, to) => { root.dataset.drag = ''; $('cb-rm').classList.remove('over'); const n = board[from]; if (!n) return; if (to < 0) remove(from); else if (to !== from) place(n, to, from); },
    });
    window.__cb3 = B3;     // handy for debugging in the console
    const tg = $('cb3-tag');
    tg.addEventListener('mouseenter', () => { tagHold = true; clearTimeout(tagTimer); });
    tg.addEventListener('mouseleave', () => { tagHold = false; tagTimer = setTimeout(() => { tagIdx = -1; paintTag(); }, 200); });
    new IntersectionObserver(es => { if (es[0].isIntersecting && !document.hidden) B3.start(); else B3.stop(); }).observe(el);   // no GPU use while the builder is off-screen
    document.addEventListener('visibilitychange', () => { if (document.hidden) B3.stop(); else if (el.offsetParent) B3.start(); });
  }
  function sync3d() {
    B3.setBoard(board, champ);
    const ov = $('cb3-ov'); ov.querySelectorAll('.cb3-pins').forEach(x => x.remove());
    board.forEach((n, i) => { if (n && gear[i] && gear[i].length) { const d = document.createElement('div'); d.className = 'cb3-pins'; d.dataset.i = i; d.innerHTML = gear[i].map((g, k) => pin(g, i, k)).join(''); ov.appendChild(d); } });
    if (tagIdx >= 0 && !board[tagIdx]) tagIdx = -1; paintTag();
  }
  function paintTag() {
    const t = $('cb3-tag'); if (!t) return;
    const c = tagIdx >= 0 && board[tagIdx] && champ(board[tagIdx]);
    t.hidden = !c; if (!c) return;
    t.style.setProperty('--c', COST[c.cost]);
    t.innerHTML = `<div class="t1"><b>${esc(c.name)}</b><em>${c.cost}</em><button data-rm="${tagIdx}" title="Remove from board">×</button></div><div class="t2">${c.traits.map(esc).join(' · ')}</div>`;
  }
  function frame3d() {     // keep the DOM overlays (item pins, name tag) glued to the champions
    document.querySelectorAll('#cb3-ov .cb3-pins').forEach(p => { const s = B3.screenOf(+p.dataset.i); if (s) p.style.transform = `translate(${s.x.toFixed(1)}px,${(s.y + 3).toFixed(1)}px) translate(-50%,0)`; });
    if (tagIdx >= 0) { const s = B3.screenOf(tagIdx), t = $('cb3-tag'); if (s && t) t.style.transform = `translate(${s.topX.toFixed(1)}px,${(s.topY - 4).toFixed(1)}px) translate(-50%,-100%)`; }
  }""")
h = sub(h, "      const h = e.target.closest('.cb-hex'), rm = e.target.closest('.cb-rm'), bg = e.target.closest('.em-bag'), out = drag.kind === 'unit' || drag.kind === 'pin';",
        "      const hi = hexOf(e), h = hi >= 0, rm = e.target.closest('.cb-rm'), bg = e.target.closest('.em-bag'), out = drag.kind === 'unit' || drag.kind === 'pin';")
h = sub(h, "drag.kind === 'champ' || drag.kind === 'unit' || !!board[+h.dataset.i]) : out;", "drag.kind === 'champ' || drag.kind === 'unit' || !!board[hi]) : out;")
h = sub(h, "      if (ok) { e.preventDefault(); if (h) h.classList.add('over'); else if (rm)", "      if (ok) { e.preventDefault(); if (h) { if (B3) B3.setDropHover(hi); else e.target.closest('.cb-hex').classList.add('over'); } else if (rm)")
h = sub(h, "      const h = e.target.closest('.cb-hex'), i = h && +h.dataset.i, d = drag; done();", "      const i = hexOf(e), h = i >= 0, d = drag; done();")
# 6. controls / tag clicks
h = sub(h, "      if (fc) { itemF = fc.dataset.f; paintItems(); }",
"""      const rm3 = g.closest('[data-rm]'), cel = g.closest('[data-cel]'), rv = g.closest('[data-rv]');
      if (rm3) { remove(+rm3.dataset.rm); tagIdx = -1; paintTag(); }
      else if (cel && B3) { B3.celebrate(!B3.isCelebrating()); cel.classList.toggle('on', B3.isCelebrating()); }
      else if (rv && B3) B3.resetView();
      else if (fc) { itemF = fc.dataset.f; paintItems(); }""")
# 7. init: icons manifest before the first paint
h = sub(h, "    shell(); wire(); wireRecs(); wireExtra(); paintSaved(); paintItems(); paintTraits(); paintPool(); loadRecs(); loadMeta();",
        "    try { MF3 = await (await fetch('models/manifest.json')).json(); } catch { MF3 = null; }   // which champions have 3D models / icons\n    shell(); wire(); wireRecs(); wireExtra(); paintSaved(); paintItems(); paintTraits(); paintPool(); loadRecs(); loadMeta();")
# 8. CSS
css = r'''
/* ---- Comp Builder: 3D board ---- */
.cb-board.is3d{display:block;position:relative;padding:0;overflow:hidden;background:radial-gradient(ellipse at 50% 35%,rgba(248,169,0,.10),transparent 68%),rgba(8,11,20,.72)}
.cb-3d{position:relative;width:100%;height:clamp(340px,52vh,480px)}
.cb3-canvas{display:block;width:100%;height:100%;touch-action:none;outline:none}
.cb3-ov{position:absolute;inset:0;overflow:hidden;pointer-events:none}
.cb3-pins{position:absolute;left:0;top:0;display:flex;gap:2px;z-index:3;pointer-events:auto;will-change:transform}
.cb3-tag{position:absolute;left:0;top:0;z-index:5;pointer-events:auto;min-width:96px;max-width:210px;padding:7px 10px 7px 12px;border-radius:14px;background:rgba(12,15,24,.92);border:1px solid color-mix(in srgb,var(--c) 70%,transparent);box-shadow:0 8px 22px rgba(0,0,0,.5),0 0 18px color-mix(in srgb,var(--c) 30%,transparent);backdrop-filter:blur(8px);will-change:transform}
.cb3-tag[hidden]{display:none}
.cb3-tag .t1{display:flex;align-items:center;gap:7px}
.cb3-tag b{font-size:13px;white-space:nowrap}
.cb3-tag em{font:800 11px var(--body);font-style:normal;color:#10131c;background:var(--c);border-radius:99px;padding:1px 7px}
.cb3-tag button{margin-left:auto;width:20px;height:20px;border:0;border-radius:50%;background:rgba(255,255,255,.1);color:#cdd3de;font-size:15px;line-height:1;cursor:pointer;transition:background .15s,color .15s}
.cb3-tag button:hover{background:#e5484d;color:#fff}
.cb3-tag .t2{margin-top:3px;font-size:10.5px;line-height:1.3;color:var(--muted)}
.cb3-ctl{position:absolute;right:12px;top:12px;z-index:6;display:flex;gap:8px}
.cb3-b{display:flex;align-items:center;gap:7px;height:34px;padding:0 13px;border-radius:99px;border:1px solid rgba(255,255,255,.16);background:rgba(12,15,24,.7);backdrop-filter:blur(8px);color:var(--text);font:700 12.5px var(--body);cursor:pointer;transition:border-color .2s,background .2s,transform .2s var(--ease)}
.cb3-b svg{width:15px;height:15px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.cb3-b:hover{border-color:var(--gold);transform:translateY(-1px)}
.cb3-b.on{background:rgba(248,169,0,.2);border-color:var(--gold);color:var(--gold)}
.cb3-hint{position:absolute;left:14px;bottom:10px;z-index:2;font-size:11.5px;color:rgba(205,211,222,.55);pointer-events:none;max-width:70%}
.cb-board.is3d .cb-rm{position:absolute;left:50%;bottom:14px;transform:translate(-50%,8px)}
#cbx[data-drag=unit] .cb-board.is3d .cb-rm,#cbx[data-drag=pin] .cb-board.is3d .cb-rm{transform:translate(-50%,0)}
.cb-board.is3d .cb-rm.over{transform:translate(-50%,0) scale(1.05)}
.cb3-ov .cb-it{pointer-events:auto}
'''
h = sub(h, "</style>", css + "</style>")
open(p, 'w', encoding='utf-8').write(h)
print('3D integration applied to', p)
