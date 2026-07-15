import React, { useState, useEffect, useMemo } from 'react';

// Live sky background for the mobile home screen. Keeps the original soft, atmospheric art
// style — a colored, misty sky up top with a soft-glowing sun/moon, fading to near-black at
// the base where the building sits — but instead of a single static image it snaps between
// five FIXED time-of-day presets: golden hour, morning, afternoon, sunset, evening. The site's
// real local time (and today's sunrise/sunset) selects the phase; crossing a boundary eases
// from one hand-tuned scene to the next.

// "2026-07-14T05:58" -> 5.966 local wall-clock hours; null if unparseable.
const parseHM = (s) => {
  if (!s || !s.includes('T')) return null;
  const [h, m] = s.split('T')[1].split(':').map(Number);
  return h + (m || 0) / 60;
};

// Soft horizontal mist wisps (the signature of the original art), tinted per phase.
const mist = (r, g, b) => (
  `radial-gradient(130% 20% at 32% 26%, rgba(${r},${g},${b},0.16), transparent 60%),`
  + `radial-gradient(150% 16% at 72% 33%, rgba(${r},${g},${b},0.12), transparent 60%),`
  + `radial-gradient(110% 13% at 52% 19%, rgba(${r},${g},${b},0.10), transparent 55%)`
);

// Five fixed scenes. Each pins a sky gradient (colored up top, near-black at the base), a
// tinted mist layer, corner ambience, and a soft sun/moon at a fixed spot high in the sky.
const PHASES = {
  golden: {
    sky: 'linear-gradient(180deg,#4a4a7a 0%,#9a6f7e 22%,#6a4650 40%,#160f1f 62%,#030209 100%)',
    mist: mist(235, 182, 150),
    glow: 'radial-gradient(circle at 22% 30%, rgba(255,190,120,0.30), transparent 55%), radial-gradient(circle at 82% 78%, rgba(120,120,180,0.16), transparent 58%)',
    body: 'sun', left: 18, top: 30,
    sun: 'radial-gradient(circle at 50% 50%, rgba(255,238,205,0.96) 0%, rgba(255,190,120,0.55) 30%, rgba(255,150,90,0.16) 55%, transparent 72%)',
  },
  morning: {
    sky: 'linear-gradient(180deg,#2f5ea0 0%,#5080b6 24%,#385a7c 46%,#101a2c 64%,#02040a 100%)',
    mist: mist(185, 208, 236),
    glow: 'radial-gradient(circle at 26% 24%, rgba(160,205,255,0.30), transparent 55%), radial-gradient(circle at 82% 80%, rgba(96,140,220,0.16), transparent 58%)',
    body: 'sun', left: 26, top: 22,
    sun: 'radial-gradient(circle at 50% 50%, rgba(255,248,225,0.96) 0%, rgba(255,222,150,0.50) 30%, rgba(255,205,130,0.14) 55%, transparent 72%)',
  },
  afternoon: {
    sky: 'linear-gradient(180deg,#1f6ec6 0%,#4184cc 26%,#2f5f92 48%,#101d30 66%,#02040a 100%)',
    mist: mist(198, 222, 246),
    glow: 'radial-gradient(circle at 50% 16%, rgba(185,218,255,0.32), transparent 52%), radial-gradient(circle at 82% 82%, rgba(110,150,225,0.15), transparent 58%)',
    body: 'sun', left: 50, top: 12,
    sun: 'radial-gradient(circle at 50% 50%, rgba(255,252,238,0.98) 0%, rgba(255,232,170,0.50) 28%, rgba(255,220,150,0.12) 52%, transparent 70%)',
  },
  sunset: {
    sky: 'linear-gradient(180deg,#43356e 0%,#9a5566 24%,#c56b48 40%,#5a3838 52%,#150d15 68%,#030208 100%)',
    mist: mist(240, 165, 120),
    glow: 'radial-gradient(circle at 82% 30%, rgba(255,150,90,0.32), transparent 55%), radial-gradient(circle at 20% 20%, rgba(120,90,160,0.20), transparent 58%)',
    body: 'sun', left: 82, top: 30,
    sun: 'radial-gradient(circle at 50% 50%, rgba(255,225,180,0.96) 0%, rgba(255,150,90,0.55) 28%, rgba(230,90,70,0.18) 54%, transparent 72%)',
  },
  evening: {
    sky: 'linear-gradient(180deg,#2b3152 0%,#1c2540 26%,#0e1626 46%,#060a16 62%,#010207 100%)',
    mist: mist(150, 172, 205),
    glow: 'radial-gradient(circle at 78% 18%, rgba(90,120,200,0.20), transparent 55%), radial-gradient(circle at 20% 78%, rgba(40,70,120,0.14), transparent 58%)',
    body: 'moon', left: 82, top: 14,
    night: true,
  },
};

// Select a fixed phase from the local hour, anchored to real sunrise/sunset.
const pickPhase = (h, sr, ss) => {
  if (h >= sr - 0.6 && h < sr + 1.4) return 'golden';
  if (h >= sr + 1.4 && h < 11) return 'morning';
  if (h >= 11 && h < ss - 1.6) return 'afternoon';
  if (h >= ss - 1.6 && h < ss + 0.8) return 'sunset';
  return 'evening';
};

export default function LiveWeatherBackground({ lat = 10.8231, lon = 106.6297 }) {
  const [meta, setMeta] = useState(null);   // { off, sunrise, sunset }
  const [nowMs, setNowMs] = useState(Date.now());

  // Today's sunrise/sunset + UTC offset for this exact site (Open-Meteo, no key). timezone=auto
  // returns local wall-clock times; the offset lets us place "now" in the site's local time too.
  useEffect(() => {
    let mounted = true;
    const url = `https://api.open-meteo.com/v1/forecast?latitude=${lat}&longitude=${lon}&daily=sunrise,sunset&timezone=auto`;
    (async () => {
      try {
        const d = await (await fetch(url)).json();
        if (mounted) setMeta({ off: d.utc_offset_seconds, sunrise: parseHM(d.daily?.sunrise?.[0]), sunset: parseHM(d.daily?.sunset?.[0]) });
      } catch (err) {
        console.error('Failed to fetch sunrise/sunset', err);
        if (mounted) setMeta(null); // fall back to the ICT defaults below
      }
    })();
    return () => { mounted = false; };
  }, [lat, lon]);

  // Re-check the phase every minute so it flips when a boundary is crossed (no arc drift).
  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 60 * 1000);
    return () => clearInterval(id);
  }, []);

  // Stable, sparse starfield (evening only).
  const stars = useMemo(
    () => Array.from({ length: 30 }, (_, i) => ({
      x: (i * 41 + 9) % 100, y: (i * 57 + 5) % 52, r: 0.6 + ((i * 7) % 3) * 0.5, delay: (i % 7) * 0.8,
    })),
    [],
  );

  // Fallbacks are Ho Chi Minh City's near-constant tropical sunrise/sunset (it barely moves
  // year-round), so the first paint picks the same phase the fetch will confirm — no flash.
  const off = meta?.off ?? 7 * 3600;            // ICT (UTC+7)
  const sr = meta?.sunrise ?? 5.6;
  const ss = meta?.sunset ?? 18.3;
  const loc = new Date(nowMs + off * 1000);
  const h = loc.getUTCHours() + loc.getUTCMinutes() / 60;
  const key = pickPhase(h, sr, ss);
  const P = PHASES[key];

  const bodyWrap = {
    position: 'absolute', left: `${P.left}%`, top: `${P.top}%`, transform: 'translate(-50%,-50%)',
    transition: 'left 4s ease, top 4s ease', pointerEvents: 'none',
  };

  return (
    <>
      <style>
        {`
          .sky-root { position:absolute; inset:0; overflow:hidden; pointer-events:none; z-index:0; transition: background 4s ease; }
          @keyframes safeDrift { 0%{transform:translate3d(0,0,0);} 50%{transform:translate3d(2%,1.5%,0);} 100%{transform:translate3d(0,0,0);} }
          .sky-layer { position:absolute; inset:-12%; width:124%; height:124%; pointer-events:none; transition: background 4s ease; }
          .sky-mist { filter: blur(5px); animation: safeDrift 30s ease-in-out infinite alternate; will-change:transform; }
          @keyframes twinkle { 0%,100%{opacity:0.25;} 50%{opacity:0.9;} }
          .sky-star { position:absolute; border-radius:50%; background:#eaf2ff; animation: twinkle 4s ease-in-out infinite; }
        `}
      </style>

      <div className="sky-root" style={{ background: P.sky }}>
        {/* corner ambience + drifting mist wisps */}
        <div className="sky-layer" style={{ background: P.glow }} />
        <div className="sky-layer sky-mist" style={{ background: P.mist }} />

        {/* stars, fading in only in the evening */}
        <div style={{ position: 'absolute', inset: 0, opacity: P.night ? 1 : 0, transition: 'opacity 4s ease' }}>
          {stars.map((s, i) => (
            <span key={i} className="sky-star" style={{ left: `${s.x}%`, top: `${s.y}%`, width: `${s.r * 2}px`, height: `${s.r * 2}px`, animationDelay: `${s.delay}s` }} />
          ))}
        </div>

        {/* soft sun by day, a soft-glowing crescent moon in the evening */}
        {P.body === 'sun' ? (
          <div style={{ ...bodyWrap, width: '150px', height: '150px', borderRadius: '50%', background: P.sun }} />
        ) : (
          <div style={{ ...bodyWrap, width: '64px', height: '64px', borderRadius: '50%', boxShadow: '0 0 44px 12px rgba(244,238,220,0.28), 0 0 100px 34px rgba(180,190,225,0.13)' }}>
            {/* lit disc */}
            <div style={{ position: 'absolute', inset: 0, borderRadius: '50%', background: 'radial-gradient(circle at 46% 44%, #fdf8ee 0%, #ece3d2 55%, rgba(224,214,194,0.3) 78%, transparent 88%)' }} />
            {/* shadow disc — filled with the sky colour so the unlit limb melts into the night */}
            <div style={{ position: 'absolute', inset: 0, borderRadius: '50%', transform: 'translate(13px,-6px)', filter: 'blur(3px)', background: 'radial-gradient(circle at 50% 50%, #242b4a 0%, #242b4a 60%, transparent 80%)' }} />
          </div>
        )}
      </div>
    </>
  );
}
