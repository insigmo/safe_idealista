// Runs in the page's main world (manifest "world": "MAIN") so it can reach
// google.maps. It captures Idealista's map instance and draws the hex overlay
// sent by the content script. Everything is wrapped so a failure only logs.
import { LOG, TAG, type OverlayMsg } from './protocol';

const maps = new Set<google.maps.Map>();
let latest: OverlayMsg | null = null;
let overlay: (google.maps.OverlayView & { redraw(): void }) | null = null;
let attachedTo: google.maps.Map | null = null;
let legend: HTMLDivElement | null = null;

function warn(...a: unknown[]): void {
  console.warn(LOG, ...a);
}

// The biggest map still in the document (Idealista has one, but stay safe).
function currentMap(): google.maps.Map | null {
  let best: google.maps.Map | null = null;
  let bestArea = 0;
  for (const m of maps) {
    const div = m.getDiv();
    if (!div.isConnected) {
      maps.delete(m);
      continue;
    }
    const area = div.offsetWidth * div.offsetHeight;
    if (area > bestArea) {
      best = m;
      bestArea = area;
    }
  }
  return best;
}

// No global holds the map, but every instance calls MVCObject#get constantly,
// so wrapping it on Map.prototype hands us `this` within a frame or two.
function hookMapPrototype(): void {
  const proto = google.maps.Map.prototype as unknown as Record<string, unknown>;
  const orig = proto.get as (this: google.maps.Map, ...a: unknown[]) => unknown;
  if (typeof orig !== 'function' || (orig as { __bcnsq?: boolean }).__bcnsq) return;
  const wrapped = function (this: google.maps.Map, ...a: unknown[]): unknown {
    if (this instanceof google.maps.Map && !maps.has(this)) {
      maps.add(this);
      queueMicrotask(sync);
    }
    return orig.apply(this, a);
  };
  (wrapped as unknown as { __bcnsq: boolean }).__bcnsq = true;
  proto.get = wrapped;
}

function makeOverlay(): google.maps.OverlayView & { redraw(): void } {
  class HexOverlay extends google.maps.OverlayView {
    private canvas = document.createElement('canvas');

    onAdd(): void {
      Object.assign(this.canvas.style, { position: 'absolute', pointerEvents: 'none' });
      this.getPanes()?.overlayLayer.appendChild(this.canvas);
    }

    onRemove(): void {
      this.canvas.remove();
    }

    redraw(): void {
      this.draw();
    }

    draw(): void {
      try {
        this.paint();
      } catch (e) {
        warn('overlay draw failed', e);
      }
    }

    private paint(): void {
      const proj = this.getProjection();
      const map = this.getMap() as google.maps.Map | null;
      const bounds = map?.getBounds();
      const ctx = this.canvas.getContext('2d');
      if (!proj || !bounds || !ctx || !latest) return;
      const sw = proj.fromLatLngToDivPixel(bounds.getSouthWest());
      const ne = proj.fromLatLngToDivPixel(bounds.getNorthEast());
      if (!sw || !ne) return;
      const x0 = Math.min(sw.x, ne.x);
      const y0 = Math.min(sw.y, ne.y);
      const w = Math.ceil(Math.abs(ne.x - sw.x));
      const h = Math.ceil(Math.abs(ne.y - sw.y));
      const dpr = window.devicePixelRatio || 1;
      Object.assign(this.canvas.style, { left: `${x0}px`, top: `${y0}px`, width: `${w}px`, height: `${h}px` });
      this.canvas.width = Math.round(w * dpr);
      this.canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);
      if (!latest.enabled) return;
      ctx.globalAlpha = latest.opacity;
      ctx.lineWidth = 0.5;
      ctx.strokeStyle = 'rgba(0,0,0,0.25)';
      for (const c of latest.cells) {
        ctx.beginPath();
        for (let i = 0; i < c.ring.length; i += 2) {
          const p = proj.fromLatLngToDivPixel(new google.maps.LatLng(c.ring[i], c.ring[i + 1]));
          if (!p) continue;
          if (i === 0) ctx.moveTo(p.x - x0, p.y - y0);
          else ctx.lineTo(p.x - x0, p.y - y0);
        }
        ctx.closePath();
        ctx.fillStyle = c.color;
        ctx.fill();
        ctx.stroke();
      }
    }
  }
  return new HexOverlay();
}

function updateLegend(map: google.maps.Map): void {
  if (!latest) return;
  if (!legend) {
    legend = document.createElement('div');
    legend.style.cssText =
      'margin:0 0 24px 10px;padding:6px 8px;background:#fff;border-radius:4px;box-shadow:0 1px 4px rgba(0,0,0,.3);font:12px/1.3 system-ui,sans-serif;color:#222';
    map.controls[google.maps.ControlPosition.LEFT_BOTTOM].push(legend);
  }
  legend.style.display = latest.enabled ? '' : 'none';
  const stops = Array.from({ length: 6 }, (_, i) => `hsl(${120 - i * 24},75%,45%)`).join(',');
  legend.innerHTML = '';
  const title = document.createElement('div');
  title.textContent = `BCN Street Quality · ${latest.title}`;
  title.style.fontWeight = '600';
  const bar = document.createElement('div');
  bar.style.cssText = `height:8px;margin:4px 0 2px;border-radius:2px;background:linear-gradient(90deg,${stops})`;
  const ends = document.createElement('div');
  ends.style.cssText = 'display:flex;justify-content:space-between;color:#555';
  ends.innerHTML = '<span>better</span><span>worse</span>';
  legend.append(title, bar, ends);
}

function sync(): void {
  try {
    const map = currentMap();
    if (!map || !latest) return;
    if (attachedTo !== map) {
      overlay?.setMap(null);
      overlay = makeOverlay();
      overlay.setMap(map);
      attachedTo = map;
      legend = null;
      console.info(LOG, 'map overlay attached');
    }
    updateLegend(map);
    overlay?.redraw();
  } catch (e) {
    warn('overlay sync failed', e);
  }
}

window.addEventListener('message', (e: MessageEvent) => {
  if (e.source !== window || !e.data || e.data[TAG] !== 'overlay') return;
  latest = e.data as OverlayMsg;
  sync();
});

// google.maps is loaded lazily by Idealista's React map component; poll for
// it cheaply and give up after two minutes on pages that never show a map.
const started = Date.now();
const timer = window.setInterval(() => {
  try {
    if (window.google?.maps?.Map) {
      window.clearInterval(timer);
      hookMapPrototype();
    } else if (Date.now() - started > 120_000) {
      window.clearInterval(timer);
    }
  } catch (e) {
    window.clearInterval(timer);
    warn('map hook failed', e);
  }
}, 300);
