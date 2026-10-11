"""Renders the champion icons (frontend/models/portraits/*.webp) from the 3D models, using headless Chromium.
Usage (from the repo root):  python3 tools/render_portraits.py
Needs: pip install playwright pillow && playwright install chromium
"""
import base64, io, json, os, subprocess, sys, time
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
from functools import partial
from threading import Thread
from playwright.sync_api import sync_playwright
from PIL import Image

root = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', 'frontend')
names = [c['name'] for c in json.load(open(os.path.join(root, 'data', 'set18.json'), encoding='utf-8'))['champions']]
os.makedirs(os.path.join(root, 'models', 'portraits'), exist_ok=True)
page = os.path.join(root, '_portraits.html')
open(page, 'w').write('<!doctype html><meta charset="utf-8"><script src="lib/cb3d.js"></script>')
class Quiet(SimpleHTTPRequestHandler):
    def log_message(self, *a): pass
srv = ThreadingHTTPServer(('127.0.0.1', 0), partial(Quiet, directory=root)); Thread(target=srv.serve_forever, daemon=True).start()
try:
    with sync_playwright() as p:
        b = p.chromium.launch(args=['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'])
        pg = b.new_page(); pg.goto(f'http://127.0.0.1:{srv.server_port}/_portraits.html')
        res = pg.evaluate('(n) => CB3D.renderPortraits("models/", n, 128)', names); b.close()
finally:
    srv.shutdown(); os.remove(page)
ok = 0
for n in names:
    d = res.get(''.join(ch for ch in n.lower() if ch.isalnum()))
    if not d: print('FAILED', n); continue
    Image.open(io.BytesIO(base64.b64decode(d.split(',')[1]))).convert('RGBA').save(os.path.join(root, 'models', 'portraits', ''.join(ch for ch in n.lower() if ch.isalnum()) + '.webp'), 'WEBP', quality=86, method=6); ok += 1
print(f'{ok}/{len(names)} portraits written')
