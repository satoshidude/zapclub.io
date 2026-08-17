package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr/nip19"
)

const (
	icyMetaint  = 8192                              // ICY metadata interval in bytes
	icyArtwork  = "https://zapclub.io/icon-192.png" // album art sent to ICY-aware players
	icyStationURL = "https://zapclub.io/"
)

// Shared turntable SVG and brand CSS used by both radio page templates.
// The vinyl group animates at 2.4 s per rotation, matching the Svelte Turntable component.
const radioBrandSVG = `<svg class="turntable" viewBox="0 0 36 36" width="240" height="240" role="img" aria-label="zapclub.io">
  <g class="vinyl">
    <circle cx="16" cy="20" r="13" fill="#1b0b33" stroke="#8e30eb" stroke-width="1.6"/>
    <circle cx="16" cy="20" r="9.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.4"/>
    <circle cx="16" cy="20" r="6.5" fill="none" stroke="#a855f7" stroke-width="0.5" opacity="0.3"/>
    <circle cx="16" cy="20" r="3.6" fill="#22c55e"/>
    <circle cx="16" cy="11.5" r="1.1" fill="#d8b4fe"/>
    <circle cx="16" cy="20" r="1" fill="#1b0b33"/>
  </g>
  <line x1="29" y1="7" x2="20.5" y2="15.5" stroke="#c084fc" stroke-width="1.7" stroke-linecap="round"/>
  <circle cx="29" cy="7" r="1.9" fill="#c084fc"/>
</svg>`

const radioBrandCSS = `
.brand{display:flex;flex-direction:column;align-items:center;gap:.9rem}
.turntable{display:block;filter:drop-shadow(0 0 10px rgba(142,48,235,.6))}
.vinyl{transform-origin:16px 20px;animation:spin 2.4s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}
@media(prefers-reduced-motion:reduce){.vinyl{animation:none}}
.brand-name{font-size:2rem;font-weight:800;letter-spacing:-.02em}
.brand-name .word{color:#fff}
.brand-name .tld{color:#8e30eb;font-weight:700}`

// radioPlayerPage uses {{CLUBID}} and {{CLUBNAME}} placeholders (replaced via strings.NewReplacer).
// No fmt.Sprintf args — avoids %% escaping issues with CSS.
const radioPlayerPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#0d0d0f">
<title>📻 {{CLUBNAME}} — zapclub.io</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d0d0f;color:#e2e8f0;font-family:system-ui,sans-serif;
     display:flex;flex-direction:column;align-items:center;justify-content:center;
     min-height:100vh;gap:1.5rem;padding:2rem 1rem}
` + radioBrandCSS + `
.now-playing{text-align:center;max-width:360px;min-height:2.2rem}
.np-title{font-size:1.05rem;font-weight:600;color:#e2e8f0;line-height:1.35}
.player-ctrl{display:flex;align-items:center;gap:.9rem}
.btn-play{height:3rem;padding:0 1.2rem;border-radius:1.5rem;border:none;
          background:#8e30eb;color:#fff;font-size:1rem;font-weight:600;cursor:pointer;
          display:flex;align-items:center;justify-content:center;flex-shrink:0;
          font-family:inherit;letter-spacing:.01em}
.btn-play:hover{background:#a855f7}
.live-badge{font-size:.68rem;font-weight:700;letter-spacing:.07em;
            padding:.2rem .5rem;border-radius:.25rem;text-transform:uppercase}
.live-badge.live{background:#ef4444;color:#fff}
.live-badge.offline{color:#64748b;border:1px solid #334155}
.vol{width:88px;accent-color:#8e30eb;cursor:pointer}
.offline-msg{color:#ef4444;font-size:.82rem;margin-top:-.5rem}
.zap-dj{background:transparent;border:1px solid #d97706;border-radius:.45rem;
        color:#fbbf24;font-size:.9rem;font-weight:700;padding:.5rem 1rem;
        cursor:pointer;display:none;align-items:center;
        gap:.4rem;font-family:inherit;letter-spacing:.01em;border:none;
        background:transparent}
.zap-dj{border:1px solid #d97706;border-radius:.45rem;background:transparent}
.zap-dj:hover{background:#1a1505;color:#fde68a;border-color:#f59e0b}
.zap-dj .bolt{font-size:1rem}
.zap-dj .lbl{font-size:.88rem}
.zap-dj .dj-name{padding-left:.55rem;margin-left:.15rem;
                  border-left:1px solid #92400e;font-size:.88rem}
.actions{display:flex;gap:.5rem;flex-wrap:wrap;justify-content:center}
.act-btn{background:#1e1e2e;border:1px solid #334155;border-radius:.4rem;
         color:#94a3b8;font-size:.8rem;padding:.45rem .85rem;cursor:pointer;
         text-decoration:none;display:inline-flex;align-items:center;gap:.3rem;
         white-space:nowrap;font-family:inherit}
.act-btn:hover{border-color:#475569;color:#e2e8f0}
.act-btn.copied{color:#22c55e;border-color:#22c55e}
.enter{background:#8e30eb;color:#fff;font-weight:600;font-size:.95rem;
       padding:.65rem 1.5rem;border-radius:.5rem;text-decoration:none;
       letter-spacing:.01em;display:inline-flex;align-items:center;gap:.35rem}
.enter:hover{background:#a855f7}
dialog{background:#0d0d0f;border:1px solid #1e293b;border-radius:.75rem;
       color:#e2e8f0;padding:0;max-width:320px;width:90vw;font-family:system-ui,sans-serif}
dialog::backdrop{background:rgba(0,0,0,.6)}
.modal-inner{padding:1.25rem;display:flex;flex-direction:column;gap:.9rem}
.modal-head{display:flex;align-items:center;justify-content:space-between}
.modal-head span{font-weight:700;font-size:1rem}
.modal-close{background:none;border:none;color:#64748b;font-size:1.1rem;cursor:pointer;padding:.1rem .3rem}
.modal-close:hover{color:#e2e8f0}
.zap-amounts{display:flex;gap:.4rem;align-items:center;flex-wrap:wrap}
.zap-amt{background:#1e293b;border:1px solid #334155;border-radius:.35rem;
         color:#94a3b8;font-size:.85rem;font-weight:600;padding:.35rem .7rem;
         cursor:pointer;font-family:inherit;transition:border-color .15s,color .15s}
.zap-amt:hover,.zap-amt.sel{border-color:#d97706;color:#fbbf24}
.sats-lbl{font-size:.78rem;color:#475569;margin-left:.2rem}
.zap-err{color:#ef4444;font-size:.8rem;margin:0}
.zap-btns{display:flex;flex-direction:column;gap:.45rem}
.btn-alby{background:#d97706;border:none;border-radius:.4rem;color:#0d0d0f;
          font-size:.95rem;font-weight:700;padding:.6rem 1.2rem;cursor:pointer;
          font-family:inherit;transition:opacity .15s;width:100%;display:flex;
          align-items:center;justify-content:center;gap:.4rem}
.btn-alby:hover:not(:disabled){opacity:.85}
.btn-alby:disabled{opacity:.5;cursor:default}
.btn-lightning{background:#1e293b;border:1px solid #334155;border-radius:.4rem;
               color:#94a3b8;font-size:.82rem;padding:.45rem 1rem;cursor:pointer;
               font-family:inherit;width:100%;transition:border-color .15s,color .15s}
.btn-lightning:hover:not(:disabled){border-color:#475569;color:#e2e8f0}
.btn-lightning:disabled{opacity:.5;cursor:default}
.inv-row{display:flex;flex-direction:column;gap:.35rem}
.inv-code{font-size:.6rem;word-break:break-all;color:#475569;
          background:#0a0a0f;border:1px solid #1e293b;border-radius:.3rem;
          padding:.4rem .5rem;max-height:3.5em;overflow:hidden;font-family:monospace}
.copy-inv{background:#1e293b;border:1px solid #334155;border-radius:.3rem;
          color:#94a3b8;font-size:.75rem;padding:.2rem .6rem;cursor:pointer;
          font-family:inherit;align-self:flex-start}
.copy-inv:hover{color:#e2e8f0}
.tap-overlay{position:fixed;inset:0;background:rgba(0,0,0,.72);display:none;
             flex-direction:column;align-items:center;justify-content:center;
             gap:1rem;cursor:pointer;z-index:99}
.tap-overlay .tap-icon{font-size:3.5rem;line-height:1}
.tap-overlay .tap-lbl{color:#e2e8f0;font-size:1.1rem;font-weight:600;letter-spacing:.02em}
</style>
</head>
<body>
<div class="brand">
` + radioBrandSVG + `
  <span class="brand-name"><span class="word">zapclub</span><span class="tld">.io</span></span>
</div>

<div class="now-playing">
  <div class="np-title" id="np-title">Connecting…</div>
</div>

<audio id="audio" preload="none"></audio>

<div class="tap-overlay" id="tap-overlay" onclick="tapStart()">
  <span class="tap-icon">▶</span>
  <span class="tap-lbl">Tap to start stream</span>
</div>

<span class="live-badge offline" id="live-badge">Offline</span>
<div class="player-ctrl">
  <button class="btn-play" id="btn-play" onclick="userToggle()" title="Play / Pause">▶ Play</button>
  <input class="vol" type="range" id="vol" min="0" max="1" step="0.02" value="1"
         oninput="document.getElementById('audio').volume=+this.value" title="Volume">
</div>
<p class="offline-msg" id="offline-msg"></p>

<button class="zap-dj" id="zap-dj" onclick="openZap()">
  <span class="bolt">⚡</span>
  <span class="lbl">zap</span>
  <span class="dj-name" id="zap-dj-name">DJ</span>
</button>

<div class="actions">
  <button class="act-btn" id="copy-btn" onclick="copyLink()">📋 Copy link</button>
  <button class="act-btn" onclick="shareLink()">📤 Share</button>
</div>

<a class="enter" href="https://zapclub.io/club/{{CLUBID}}">↗ Enter {{CLUBNAME}}</a>

<dialog id="zap-modal" onclick="if(event.target===this)this.close()">
  <div class="modal-inner">
    <div class="modal-head">
      <span>⚡ Zap <span id="zap-modal-name"></span></span>
      <button class="modal-close" onclick="document.getElementById('zap-modal').close()">✕</button>
    </div>
    <div class="zap-amounts">
      <button class="zap-amt" data-amt="21" onclick="selAmt(this)">21</button>
      <button class="zap-amt sel" data-amt="100" onclick="selAmt(this)">100</button>
      <button class="zap-amt" data-amt="1000" onclick="selAmt(this)">1k</button>
      <button class="zap-amt" data-amt="5000" onclick="selAmt(this)">5k</button>
      <span class="sats-lbl">sats</span>
    </div>
    <p id="zap-err" style="display:none" class="zap-err"></p>
    <div class="zap-btns" id="zap-btns">
      <button class="btn-alby" id="btn-alby" onclick="doZap('alby')">⚡ Pay with Alby Go</button>
      <button class="btn-lightning" id="btn-lightning" onclick="doZap('lightning')">🔗 Other wallet</button>
    </div>
    <div id="inv-row" style="display:none" class="inv-row">
      <code class="inv-code" id="inv-code"></code>
      <button class="copy-inv" id="copy-inv-btn" onclick="copyInv()">📋 Copy invoice</button>
    </div>
  </div>
</dialog>

<script>
var BASE = location.href.replace(/[?#].*$/, '').replace(/\/$/, '');
var INFO = BASE + '/info';
var audio = document.getElementById('audio');
var playing = true;
var retryTimer = null;
var currentPubkey = '';
var currentAmt = 100;
var profileCache = {};

function freshSrc() { return BASE + '?_=' + Date.now(); }

function setStatus(live, msg) {
  var badge = document.getElementById('live-badge');
  if (live) { badge.textContent = 'LIVE'; badge.className = 'live-badge live'; }
  else       { badge.textContent = 'Offline'; badge.className = 'live-badge offline'; }
  document.getElementById('btn-play').textContent = live ? '⏸ Pause' : '▶ Play';
  document.getElementById('offline-msg').textContent = msg || '';
}

function connect() {
  clearTimeout(retryTimer); retryTimer = null;
  audio.src = freshSrc();
  audio.play().catch(function() {
    playing = false;
    setStatus(false, '');
    document.getElementById('tap-overlay').style.display = 'flex';
  });
}
function tapStart() {
  document.getElementById('tap-overlay').style.display = 'none';
  playing = true;
  connect();
}

function retry(delayMs) {
  if (retryTimer || !playing) return;
  retryTimer = setTimeout(connect, delayMs || 3000);
}

audio.addEventListener('stalled', function() { setStatus(false, ''); });
audio.addEventListener('waiting', function() { setStatus(false, ''); });
audio.addEventListener('ended',   function() { setStatus(false, ''); retry(1500); });
audio.addEventListener('error',   function() { setStatus(false, ''); retry(4000); });

function userToggle() {
  if (!playing || audio.paused || audio.error || audio.ended) {
    playing = true; connect();
  } else {
    playing = false;
    clearTimeout(retryTimer); retryTimer = null;
    audio.pause(); audio.src = ''; setStatus(false, '');
  }
}

function copyLink() {
  navigator.clipboard.writeText(BASE).then(function() {
    var btn = document.getElementById('copy-btn');
    btn.textContent = '✓ Copied'; btn.classList.add('copied');
    setTimeout(function() { btn.textContent = '📋 Copy link'; btn.classList.remove('copied'); }, 1800);
  }).catch(function() { prompt('Copy this URL:', BASE); });
}
function shareLink() {
  var d = { title: '{{CLUBNAME}} — zapclub.io Livestream', url: BASE };
  if (navigator.share && navigator.canShare && navigator.canShare(d)) {
    navigator.share(d).catch(function() {});
  } else { copyLink(); }
}

function showZap(name) {
  document.getElementById('zap-dj-name').textContent = name;
  document.getElementById('zap-modal-name').textContent = name;
  document.getElementById('zap-dj').style.display = 'inline-flex';
}
function hideZap() {
  document.getElementById('zap-dj').style.display = 'none';
  currentPubkey = '';
}

// Fetch Nostr profile for a hex pubkey — tries relay.nostr.band first, falls back to nos.lol.
function fetchProfile(pubkeyHex, cb) {
  if (profileCache[pubkeyHex]) { cb(profileCache[pubkeyHex]); return; }
  var done = false;
  var t = setTimeout(function() { if (!done) { done = true; cb(null); } }, 7000);
  function tryRelay(url, delay) {
    setTimeout(function() {
      if (done) return;
      try {
        var ws = new WebSocket(url);
        ws.onopen = function() {
          ws.send(JSON.stringify(["REQ","n1",{"kinds":[0],"authors":[pubkeyHex],"limit":1}]));
        };
        ws.onmessage = function(e) {
          if (done) return;
          try {
            var msg = JSON.parse(e.data);
            if (msg[0]==='EVENT' && msg[2] && msg[2].kind===0) {
              clearTimeout(t); done = true;
              try { ws.close(); } catch(x) {}
              var p = JSON.parse(msg[2].content);
              profileCache[pubkeyHex] = p;
              cb(p);
            }
          } catch(x) {}
        };
        ws.onerror = function() { try { ws.close(); } catch(x) {} };
      } catch(x) {}
    }, delay);
  }
  tryRelay('wss://relay.nostr.band', 0);
  tryRelay('wss://nos.lol', 1500);
}

function updateMediaSession(title) {
  if (!('mediaSession' in navigator)) return;
  try {
    navigator.mediaSession.metadata = new MediaMetadata({
      title: title || '{{CLUBNAME}}',
      artist: 'zapclub.io',
      artwork: [{src: 'https://zapclub.io/icon-192.png', sizes: '192x192', type: 'image/png'}]
    });
    navigator.mediaSession.playbackState = playing ? 'playing' : 'paused';
  } catch(e) {}
}
if ('mediaSession' in navigator) {
  navigator.mediaSession.setActionHandler('play', function() { playing = true; connect(); });
  navigator.mediaSession.setActionHandler('pause', function() {
    playing = false; clearTimeout(retryTimer); retryTimer = null;
    audio.pause(); audio.src = ''; setStatus(false, '');
    navigator.mediaSession.playbackState = 'paused';
  });
  navigator.mediaSession.setActionHandler('stop', function() {
    playing = false; clearTimeout(retryTimer); retryTimer = null;
    audio.pause(); audio.src = ''; setStatus(false, '');
    navigator.mediaSession.playbackState = 'none';
  });
}

// Reconnect when the tab comes back into focus (timers may have been throttled in background).
document.addEventListener('visibilitychange', function() {
  if (!document.hidden && playing && (audio.paused || audio.ended || audio.error || !audio.src)) {
    connect();
  }
});

audio.addEventListener('playing', function() {
  setStatus(true, '');
  navigator.mediaSession && (navigator.mediaSession.playbackState = 'playing');
});

function pollInfo() {
  fetch(INFO).then(function(r) { return r.json(); }).then(function(d) {
    var title = d.title || (d.active ? '{{CLUBNAME}} — Live' : '— No DJ active —');
    document.getElementById('np-title').textContent = title;
    updateMediaSession(title);
    if (!d.active) {
      document.getElementById('offline-msg').textContent = 'Stream starts as soon as a DJ is on stage.';
    } else if (document.getElementById('offline-msg').textContent === 'Stream starts as soon as a DJ is on stage.') {
      document.getElementById('offline-msg').textContent = '';
    }
    if (d.dj_pubkey) {
      var p = profileCache[d.dj_pubkey];
      var shortNpub = d.dj_npub ? d.dj_npub.slice(0,12) + '…' : d.dj_pubkey.slice(0,10) + '…';
      showZap(p ? (p.display_name || p.name || shortNpub) : shortNpub);
      if (d.dj_pubkey !== currentPubkey) {
        currentPubkey = d.dj_pubkey;
        fetchProfile(d.dj_pubkey, function(prof) {
          if (prof && currentPubkey === d.dj_pubkey)
            showZap(prof.display_name || prof.name || shortNpub);
        });
      }
    } else {
      hideZap();
    }
  }).catch(function() {});
}

// ── Zap modal ───────────────────────────────────────────────────────────────
function openZap() {
  if (!currentPubkey) return;
  document.getElementById('zap-err').style.display = 'none';
  document.getElementById('inv-row').style.display = 'none';
  document.getElementById('btn-alby').disabled = false;
  document.getElementById('btn-alby').textContent = '⚡ Pay with Alby Go';
  document.getElementById('btn-lightning').disabled = false;
  document.getElementById('btn-lightning').textContent = '🔗 Other wallet';
  document.getElementById('zap-modal').showModal();
}

var currentBolt11 = '';
function doZap(scheme) {
  var prof = profileCache[currentPubkey];
  var lud16 = prof && prof.lud16;
  if (!lud16) {
    document.getElementById('zap-err').textContent = 'No Lightning address set for this DJ.';
    document.getElementById('zap-err').style.display = 'block';
    return;
  }
  // If we already have a bolt11 for the current amount, just open it.
  if (currentBolt11) { window.location.href = scheme + ':' + currentBolt11; return; }
  var btnA = document.getElementById('btn-alby');
  var btnL = document.getElementById('btn-lightning');
  btnA.disabled = true; btnL.disabled = true;
  btnA.textContent = 'Generating…';
  document.getElementById('zap-err').style.display = 'none';
  var parts = lud16.split('@');
  if (parts.length !== 2) {
    document.getElementById('zap-err').textContent = 'Invalid Lightning address.';
    document.getElementById('zap-err').style.display = 'block';
    btnA.disabled = false; btnL.disabled = false;
    btnA.textContent = '⚡ Pay with Alby Go';
    return;
  }
  var url = 'https://' + parts[1] + '/.well-known/lnurlp/' + parts[0];
  fetch(url)
    .then(function(r) { return r.json(); })
    .then(function(data) {
      var ms = currentAmt * 1000;
      var sep = data.callback.indexOf('?') >= 0 ? '&' : '?';
      return fetch(data.callback + sep + 'amount=' + ms);
    })
    .then(function(r) { return r.json(); })
    .then(function(inv) {
      currentBolt11 = inv.pr;
      document.getElementById('inv-code').textContent = currentBolt11;
      document.getElementById('inv-row').style.display = 'flex';
      btnA.disabled = false; btnL.disabled = false;
      btnA.textContent = '⚡ Pay with Alby Go';
      window.location.href = scheme + ':' + currentBolt11;
    })
    .catch(function(e) {
      document.getElementById('zap-err').textContent = 'Error: ' + (e.message || 'could not get invoice');
      document.getElementById('zap-err').style.display = 'block';
      btnA.disabled = false; btnL.disabled = false;
      btnA.textContent = '⚡ Pay with Alby Go';
    });
}
// Reset cached invoice when amount changes.
function selAmt(btn) {
  document.querySelectorAll('.zap-amt').forEach(function(b) { b.classList.remove('sel'); });
  btn.classList.add('sel');
  currentAmt = parseInt(btn.dataset.amt, 10);
  currentBolt11 = '';
  document.getElementById('inv-row').style.display = 'none';
}

function copyInv() {
  var txt = document.getElementById('inv-code').textContent;
  navigator.clipboard.writeText(txt).then(function() {
    var btn = document.getElementById('copy-inv-btn');
    btn.textContent = '✓ Copied';
    setTimeout(function() { btn.textContent = '📋 Copy invoice'; }, 1800);
  });
}

// Safari ignores currentTime seeks on live HTTP streams; reconnect instead.
var isSafari = /^((?!chrome|android).)*safari/i.test(navigator.userAgent);

// Live-edge enforcement: runs every 2 s.
// Chrome/Firefox: seek to buffer-end − 0.5 s (low latency, no gap).
// Safari: reconnect when drift > 8 s (currentTime seek is a no-op there).
setInterval(function() {
  if (!audio.paused && audio.buffered.length > 0) {
    var end = audio.buffered.end(audio.buffered.length - 1);
    var drift = end - audio.currentTime;
    if (isSafari) {
      if (drift > 8) {
        var vol = audio.volume;
        audio.src = freshSrc();
        audio.volume = vol;
        audio.play().catch(function() {});
      }
    } else {
      if (drift > 3) {
        audio.currentTime = Math.max(0, end - 0.5);
      }
    }
  }
}, 2000);

// On Safari, jump to live edge as soon as playback starts (clears startup buffer).
if (isSafari) {
  audio.addEventListener('playing', function() {
    if (audio.buffered.length > 0) {
      var end = audio.buffered.end(audio.buffered.length - 1);
      if (end - audio.currentTime > 3) {
        var vol = audio.volume;
        audio.src = freshSrc();
        audio.volume = vol;
        audio.play().catch(function() {});
      }
    }
  }, { once: false });
}

connect();
pollInfo();
setInterval(pollInfo, 12000);
</script>
</body>
</html>`

// radioOfflinePage uses {{CLUBID}} and {{CLUBNAME}} placeholders (replaced via strings.NewReplacer).
const radioOfflinePage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>📻 {{CLUBNAME}} — zapclub.io</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{background:#0d0d0f;font-family:system-ui,sans-serif;
     display:flex;flex-direction:column;align-items:center;justify-content:center;
     min-height:100vh;gap:1.5rem;padding:2rem}
` + radioBrandCSS + `
.turntable{opacity:.4}
.vinyl{animation:none!important}
.status{color:#475569;font-size:.85rem}
.enter{display:inline-flex;align-items:center;gap:.35rem;background:#8e30eb;color:#fff;
       font-weight:600;font-size:.9rem;padding:.65rem 1.5rem;border-radius:.5rem;
       text-decoration:none;letter-spacing:.01em}
.enter:hover{background:#a855f7}
</style>
</head>
<body>
<div class="brand">
` + radioBrandSVG + `
  <span class="brand-name"><span class="word">zapclub</span><span class="tld">.io</span></span>
</div>
<p class="status">📻 Stream offline — checking…</p>
<a class="enter" href="https://zapclub.io/club/{{CLUBID}}">⚡ {{CLUBNAME}}</a>
<script>
var INFO = location.href.replace(/[?#].*$/, '').replace(/\/$/, '') + '/info';
(function poll() {
  fetch(INFO).then(function(r){return r.json();}).then(function(d){
    if (d.active) { location.reload(); return; }
    setTimeout(poll, 10000);
  }).catch(function(){ setTimeout(poll, 15000); });
})();
</script>
</body>
</html>`

// ytdlpProxy is the SOCKS5/HTTP proxy used for yt-dlp to bypass YouTube's
// datacenter IP blocks. Set via YTDLP_PROXY env (e.g. socks5://127.0.0.1:40000).
// Cloudflare WARP in proxy mode listens on socks5://127.0.0.1:40000 by default.
var ytdlpProxy = os.Getenv("YTDLP_PROXY")

// lobbyVideoID is a YouTube video ID to loop when no DJ is on stage. When
// empty the stream falls back to a silent placeholder. Set via LOBBY_VIDEO_ID env.
var lobbyVideoID = os.Getenv("LOBBY_VIDEO_ID")

// lobbyMP3Path is a local MP3 file to loop as the lobby track instead of
// downloading via yt-dlp. Zero WARP bandwidth. Set via LOBBY_MP3_PATH env.
var lobbyMP3Path = os.Getenv("LOBBY_MP3_PATH")

// radioStation fans out audio chunks from the server-side yt-dlp→ffmpeg pipeline
// to all connected HTTP listeners.
type radioStation struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	titleMu sync.RWMutex
	title   string // current track title for ICY metadata injection
}

func newRadioStation() *radioStation {
	return &radioStation{clients: map[chan []byte]struct{}{}}
}

func (s *radioStation) setTitle(t string) {
	s.titleMu.Lock()
	s.title = t
	s.titleMu.Unlock()
}

func (s *radioStation) getTitle() string {
	s.titleMu.RLock()
	defer s.titleMu.RUnlock()
	return s.title
}

// icyBlock builds an ICY metadata block for StreamTitle injection.
// Format: 1-byte length (in 16-byte units) + metadata padded to length*16 bytes.
func icyBlock(title string) []byte {
	// Strip/replace characters that break the ICY format.
	safe := strings.NewReplacer("'", "’", "\n", " ", "\r", "").Replace(title)
	meta := "StreamTitle='" + safe + "';StreamUrl='" + icyStationURL + "';"
	metaBytes := []byte(meta)
	blocks := (len(metaBytes) + 15) / 16
	if blocks > 255 {
		blocks = 255
	}
	buf := make([]byte, 1+blocks*16)
	buf[0] = byte(blocks)
	copy(buf[1:], metaBytes)
	return buf
}

func (s *radioStation) subscribe() chan []byte {
	ch := make(chan []byte, 512) // ~2 MB buffer — absorbs ~120 s of 128 kbps MP3
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *radioStation) unsubscribe(ch chan []byte) {
	s.mu.Lock()
	delete(s.clients, ch)
	s.mu.Unlock()
}

func (s *radioStation) listenerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *radioStation) broadcast(data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- cp:
		default: // slow listener: drop rather than block
		}
	}
}

// radioClub holds per-club streaming state.
type radioClub struct {
	station *radioStation
	cancel  context.CancelFunc
	gen     int64 // incremented each time a new goroutine is started; lets onStreamEnd detect replacement
	videoID string
	title   string
	dj      string // pubkey of DJ playing the current track
	enabled bool   // owner-controlled; in-memory only — streams require explicit start after relay restart
	paused  bool   // auto-paused when no DJs are on stage; cleared on first onTrackChange
}

// radioManager manages server-side audio streaming per club.
// Streams are owner-toggled (start/stop via NIP-98 POST).
// When enabled but nothing is playing, a silent placeholder keeps the stream alive.
// Enabled state is persisted in SQLite (radio_state table) and restored on startup.
type radioManager struct {
	mu    sync.Mutex
	clubs map[string]*radioClub
	sq    *sql.DB     // nil = no persistence (graceful degradation)
	cache *trackCache // nil if cache dir unavailable
}

func newRadioManager() *radioManager {
	m := &radioManager{clubs: map[string]*radioClub{}}
	cacheDir := radioCacheDir
	if cacheDir == "" {
		cacheDir = "trackcache"
	}
	m.cache = newTrackCache(cacheDir)
	return m
}

// initFromSQLite wires the DB and restores enabled state for all persisted streams.
// Call before the conductor starts ticking so onTrackChange can start streams on the
// first heartbeat without needing the owner to press Start again.
func (m *radioManager) initFromSQLite(sq *sql.DB) {
	if sq == nil {
		return
	}
	m.sq = sq
	rows, err := sq.Query(`SELECT club FROM radio_state WHERE enabled=1`)
	if err != nil {
		log.Printf("radio sqlite restore: %v", err)
		return
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var clubID string
		if err := rows.Scan(&clubID); err != nil {
			continue
		}
		rc := m.getOrCreate(clubID)
		rc.enabled = true
		n++
	}
	if n > 0 {
		log.Printf("radio sqlite: restored %d enabled stream(s)", n)
	}
}

func (m *radioManager) sqSaveEnabled(clubID string, enabled bool) {
	if m.sq == nil {
		return
	}
	val := 0
	if enabled {
		val = 1
	}
	if _, err := m.sq.Exec(`INSERT OR REPLACE INTO radio_state(club,enabled) VALUES(?,?)`, clubID, val); err != nil {
		log.Printf("radio sqlite save [%.8s]: %v", clubID, err)
	}
}

func (m *radioManager) getOrCreate(clubID string) *radioClub {
	if rc, ok := m.clubs[clubID]; ok {
		return rc
	}
	rc := &radioClub{station: newRadioStation()}
	m.clubs[clubID] = rc
	return rc
}

// onTrackChange is called by the conductor on every track advance or stop.
// videoID="" means DJ is on stage but queue is empty → play placeholder.
// Clears the auto-pause so the stream resumes after a DJ-less pause.
// Never auto-enables — only /radio/{id}/start does that.
func (m *radioManager) onTrackChange(clubID, videoID, title, dj string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc := m.getOrCreate(clubID)
	rc.paused = false // DJ is (back) on stage — resume stream
	rc.title = title
	rc.dj = dj
	rc.station.setTitle(title)
	m.startStream(rc, clubID, videoID)
}

// onNoDJs is called by the conductor when the last DJ leaves the stage.
// Pauses the stream without disabling it — auto-resumes on next onTrackChange.
func (m *radioManager) onNoDJs(clubID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc, ok := m.clubs[clubID]
	if !ok || !rc.enabled {
		return
	}
	if rc.paused {
		return // already paused
	}
	rc.paused = true
	rc.videoID = "" // reset so next startStream doesn't no-op
	if rc.cancel != nil {
		rc.cancel()
		rc.cancel = nil
	}
	log.Printf("radio [%.8s] stream paused — no DJs on stage", clubID)
}

// enabledUnpausedClubs lists clubs whose stream is enabled and not paused — the
// conductor reconciles these against its own club state each tick so a stream
// can't sit enabled-but-silent (no goroutine feeding the station) after a restart.
func (m *radioManager) enabledUnpausedClubs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, rc := range m.clubs {
		if rc.enabled && !rc.paused {
			out = append(out, id)
		}
	}
	return out
}

// isActive returns true when the club's stream is enabled (real track or placeholder).
func (m *radioManager) isActive(clubID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	rc := m.clubs[clubID]
	return rc != nil && rc.enabled
}

// streamStats returns a snapshot of all known clubs with streaming state.
func (m *radioManager) streamStats() []streamInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]streamInfo, 0, len(m.clubs))
	for id, rc := range m.clubs {
		out = append(out, streamInfo{
			ClubID:    id,
			Listeners: rc.station.listenerCount(),
			Enabled:   rc.enabled,
			Title:     rc.title,
		})
	}
	return out
}

// startStream starts or switches the stream for a club. Caller must hold m.mu.
// If disabled → cancel any running stream.
// If enabled + videoID="" → start silent placeholder (keeps listeners connected).
// If enabled + videoID!="" → start real yt-dlp→ffmpeg pipeline.
// No-op if the same videoID is already streaming (avoids restarting the placeholder
// every 15 s when the conductor bootstrap-ticks with no tracks to play).
// When a stream goroutine exits naturally (not replaced by a newer startStream or
// handleToggle stop), rc.cancel is cleared so the next conductor heartbeat with the
// same videoID restarts it; for real video streams it also falls back to placeholder.
func (m *radioManager) startStream(rc *radioClub, clubID, videoID string) {
	if rc.videoID == videoID && rc.cancel != nil {
		return // same content already running — leave it alone
	}
	if rc.cancel != nil {
		rc.cancel()
		rc.cancel = nil
	}
	rc.videoID = videoID
	if !rc.enabled || rc.paused {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rc.cancel = cancel
	rc.gen++
	myGen := rc.gen

	// runLobby streams the lobby track (blocking — call inside a goroutine).
	// Priority: local MP3 file → lobby YouTube video → silent placeholder.
	runLobby := func(ctx context.Context) {
		rc.station.setTitle("zapclub.io — Lobby")
		if lobbyMP3Path != "" {
			streamLobbyFile(ctx, rc.station, clubID, lobbyMP3Path)
		} else if lobbyVideoID != "" {
			streamLobbyVideo(ctx, m.cache, rc.station, clubID, lobbyVideoID)
		} else {
			streamPlaceholder(ctx, rc.station, clubID)
		}
	}

	// onStreamEnd is called when the goroutine exits. If this goroutine is still
	// the active one (gen not yet replaced), reset rc so the next conductor
	// heartbeat can restart it, and for real video streams fall back to placeholder
	// to keep listeners connected until the conductor advances to the next track.
	onStreamEnd := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if rc.gen != myGen {
			return // a newer startStream or handleToggle stop already took over
		}
		rc.cancel = nil
		rc.videoID = ""
		if rc.enabled && videoID != "" {
			// Real video stream ended — fall back to lobby (or silent placeholder).
			rc.gen++
			ctx2, cancel2 := context.WithCancel(context.Background())
			rc.cancel = cancel2
			go runLobby(ctx2) // no onStreamEnd — lobby loops until next startStream
		}
	}

	if videoID == "" {
		go func() { runLobby(ctx); onStreamEnd() }()
	} else {
		go func() { streamVideo(ctx, m.cache, rc.station, clubID, videoID); onStreamEnd() }()
	}
}

// streamPlaceholder generates a silent MP3 stream via ffmpeg to keep listeners connected
// when the club is enabled but no track is playing (lobby mode).
func streamPlaceholder(ctx context.Context, station *radioStation, clubID string) {
	log.Printf("radio [%.8s] placeholder start", clubID)
	defer log.Printf("radio [%.8s] placeholder stop", clubID)
	for {
		ffCmd := exec.CommandContext(ctx, "ffmpeg",
			"-loglevel", "quiet",
			"-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono",
			"-f", "mp3", "-b:a", "64k",
			"pipe:1",
		)
		stdout, err := ffCmd.StdoutPipe()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if err := ffCmd.Start(); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		buf := make([]byte, 2048)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				station.broadcast(chunk)
			}
			if readErr != nil {
				break
			}
		}
		ffCmd.Wait() //nolint:errcheck
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// streamLobbyVideo loops a YouTube video indefinitely as the lobby track.
// Called instead of streamPlaceholder when LOBBY_VIDEO_ID is set.
func streamLobbyVideo(ctx context.Context, tc *trackCache, station *radioStation, clubID, videoID string) {
	log.Printf("radio [%.8s] lobby loop start vid=%s", clubID, videoID)
	defer log.Printf("radio [%.8s] lobby loop stop vid=%s", clubID, videoID)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		streamOnce(ctx, tc, station, clubID, videoID)
		// Brief pause between loops so a quick failure doesn't busy-spin.
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// streamVideo streams a YouTube video via the track cache.
// Retries up to 3 times with backoff if the download fails (e.g. yt-dlp 429).
func streamVideo(ctx context.Context, tc *trackCache, station *radioStation, clubID, videoID string) {
	log.Printf("radio [%.8s] stream start vid=%s", clubID, videoID)

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*15) * time.Second
			log.Printf("radio [%.8s] retry %d/%d in %s vid=%s", clubID, attempt, 3, delay, videoID)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		ok, err := streamOnce(ctx, tc, station, clubID, videoID)
		if ok {
			break
		}
		if errors.Is(err, errVidUnavailable) {
			// Geo-blocked/removed via the WARP exit — retrying cannot help, and every
			// retry cycle holds the station silent. Bail straight to the lobby fallback.
			log.Printf("radio [%.8s] unavailable — skipping to lobby vid=%s", clubID, videoID)
			break
		}
	}
	log.Printf("radio [%.8s] stream end vid=%s", clubID, videoID)
}

// streamLobbyFile loops a local MP3 file using ffmpeg with zero WARP bandwidth.
// Uses -stream_loop -1 so ffmpeg loops indefinitely until ctx is cancelled.
func streamLobbyFile(ctx context.Context, station *radioStation, clubID, path string) {
	log.Printf("radio [%.8s] lobby file loop start path=%s", clubID, path)
	defer log.Printf("radio [%.8s] lobby file loop stop path=%s", clubID, path)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ffCmd := exec.CommandContext(ctx, "ffmpeg",
			"-loglevel", "quiet",
			"-re",
			"-stream_loop", "-1",
			"-i", path,
			"-vn",
			"-f", "mp3", "-c:a", "libmp3lame", "-b:a", "128k",
			"-flush_packets", "1",
			"-write_xing", "0",
			"pipe:1",
		)
		stdout, err := ffCmd.StdoutPipe()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		if err := ffCmd.Start(); err != nil {
			log.Printf("radio [%.8s] lobby file ffmpeg: %v", clubID, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
		buf := make([]byte, 2048)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				station.broadcast(chunk)
			}
			if readErr != nil {
				break
			}
		}
		ffCmd.Wait() //nolint:errcheck
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// streamOnce acquires videoID from the track cache and streams it through ffmpeg
// to all connected listeners. The cache handles dedup and serialises yt-dlp
// through the WARP proxy so only N downloads run simultaneously.
// Returns true if the stream ran ≥30 s (successful playback).
func streamOnce(ctx context.Context, tc *trackCache, station *radioStation, clubID, videoID string) (bool, error) {
	if tc == nil {
		log.Printf("radio [%.8s] streamOnce: no cache — skipping vid=%s", clubID, videoID)
		return false, nil
	}
	start := time.Now()

	e, err := tc.acquireStreaming(ctx, videoID)
	if err != nil {
		log.Printf("radio [%.8s] cache acquire vid=%s: %v", clubID, videoID, err)
		return false, err
	}
	defer tc.release(videoID)

	// Tail-read e.path while yt-dlp may still be writing. Poll on EOF until
	// e.done is closed (download complete), then signal clean EOF to ffmpeg.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		f, err := os.Open(e.path)
		if err != nil {
			log.Printf("radio [%.8s] cache open vid=%s: %v", clubID, videoID, err)
			return
		}
		defer f.Close()
		buf := make([]byte, 32768)
		for {
			n, readErr := f.Read(buf)
			if n > 0 {
				if _, werr := pw.Write(buf[:n]); werr != nil {
					return // ffmpeg stdin closed (ctx cancelled or ffmpeg exit)
				}
			}
			if readErr == io.EOF {
				select {
				case <-e.done:
					return // download complete; no more data
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
					// yt-dlp still writing — retry read
				}
			} else if readErr != nil {
				return
			}
		}
	}()

	ffCmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "warning",
		"-probesize", "65536",
		"-analyzeduration", "0",
		"-re",
		"-i", "pipe:0",
		"-vn",
		"-f", "mp3", "-c:a", "libmp3lame", "-b:a", "128k",
		"-flush_packets", "1",
		"-write_xing", "0",
		"pipe:1",
	)
	ffCmd.Stdin = pr
	stdout, err := ffCmd.StdoutPipe()
	if err != nil {
		log.Printf("radio [%.8s] ffmpeg pipe: %v", clubID, err)
		pr.Close()
		return false, err
	}
	if err := ffCmd.Start(); err != nil {
		log.Printf("radio [%.8s] ffmpeg start: %v", clubID, err)
		pr.Close()
		return false, err
	}
	defer ffCmd.Wait()

	buf := make([]byte, 2048)
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			station.broadcast(chunk)
		}
		if readErr != nil {
			break
		}
	}
	return time.Since(start) >= 30*time.Second, nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

type radioHandler struct {
	mgr  *radioManager
	cond *conductor
}

func (h *radioHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, ".m3u") && strings.HasPrefix(path, "/radio/"):
		clubID := strings.TrimSuffix(strings.TrimPrefix(path, "/radio/"), ".m3u")
		h.handleM3U(w, r, clubID)
	case strings.HasSuffix(path, "/start") && strings.HasPrefix(path, "/radio/"):
		clubID := strings.TrimSuffix(strings.TrimPrefix(path, "/radio/"), "/start")
		h.handleToggle(w, r, clubID, true)
	case strings.HasSuffix(path, "/stop") && strings.HasPrefix(path, "/radio/"):
		clubID := strings.TrimSuffix(strings.TrimPrefix(path, "/radio/"), "/stop")
		h.handleToggle(w, r, clubID, false)
	case strings.HasSuffix(path, "/info") && strings.HasPrefix(path, "/radio/"):
		clubID := strings.TrimSuffix(strings.TrimPrefix(path, "/radio/"), "/info")
		h.handleInfo(w, r, clubID)
	case strings.HasPrefix(path, "/radio/") && len(path) > len("/radio/"):
		clubID := strings.TrimPrefix(path, "/radio/")
		h.handleListen(w, r, clubID)
	default:
		http.NotFound(w, r)
	}
}

// handleToggle starts or stops a club's radio stream.
// Requires NIP-98 Authorization from the club owner (or SUPERADMIN).
func (h *radioHandler) handleToggle(w http.ResponseWriter, r *http.Request, clubID string, enable bool) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pubkey, ok := verifyNIP98Pubkey(r)
	if !ok {
		http.Error(w, "unauthorized: valid NIP-98 Authorization required", http.StatusUnauthorized)
		return
	}
	owner := h.cond.clubOwner(r.Context(), clubID)
	sa := os.Getenv("SUPERADMIN")
	if pubkey != owner && (sa == "" || pubkey != sa) {
		http.Error(w, "forbidden: club owner only", http.StatusForbidden)
		return
	}

	if enable {
		// Premium gate — livestreaming is a premium feature (SUPERADMIN exempt).
		if pubkey != sa && !h.cond.isPremiumOwner(r.Context(), clubID, time.Now().UnixMilli()) {
			http.Error(w, "premium required: upgrade to start a livestream", http.StatusPaymentRequired)
			return
		}
		// One-stream gate — owner may only run one active stream across all their clubs.
		h.mgr.mu.Lock()
		var activePeers []string
		for cid, rc := range h.mgr.clubs {
			if cid != clubID && rc.enabled {
				activePeers = append(activePeers, cid)
			}
		}
		h.mgr.mu.Unlock()
		for _, cid := range activePeers {
			if h.cond.clubOwner(r.Context(), cid) == pubkey {
				log.Printf("radio [%.8s] start blocked: owner %.8s has active stream on %.8s", clubID, pubkey, cid)
				http.Error(w, "conflict: you already have an active stream on another club — stop it first", http.StatusConflict)
				return
			}
		}
	}

	h.mgr.mu.Lock()
	rc := h.mgr.getOrCreate(clubID)
	rc.enabled = enable
	h.mgr.sqSaveEnabled(clubID, enable) // persist across restarts
	if enable {
		h.mgr.startStream(rc, clubID, rc.videoID)
	} else {
		if rc.cancel != nil {
			rc.gen++ // invalidate any in-flight onStreamEnd before cancelling
			rc.cancel()
			rc.cancel = nil
		}
	}
	h.mgr.mu.Unlock()

	action := "stopped"
	if enable {
		action = "started"
	}
	log.Printf("radio [%.8s] %s by %s", clubID, action, pubkey[:8])
	w.WriteHeader(http.StatusNoContent)
}

// handleInfo returns a JSON snapshot of the club's current stream state.
// Used by the radio player page to poll track/DJ info.
func (h *radioHandler) handleInfo(w http.ResponseWriter, r *http.Request, clubID string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/json")

	h.mgr.mu.Lock()
	rc := h.mgr.clubs[clubID]
	h.mgr.mu.Unlock()

	type infoResponse struct {
		Active   bool   `json:"active"`
		Paused   bool   `json:"paused"` // enabled but no DJs on stage — station intentionally silent
		Title    string `json:"title,omitempty"`
		DJNpub   string `json:"dj_npub,omitempty"`
		DJPubkey string `json:"dj_pubkey,omitempty"`
		Club     string `json:"club"`
	}
	resp := infoResponse{Club: h.clubName(clubID)}
	if rc != nil {
		resp.Active = rc.enabled
		resp.Paused = rc.paused
		resp.Title = rc.title
		if rc.dj != "" {
			resp.DJPubkey = rc.dj
			if npub, err := nip19.EncodePublicKey(rc.dj); err == nil {
				resp.DJNpub = npub
			}
		}
	}
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func (h *radioHandler) handleListen(w http.ResponseWriter, r *http.Request, clubID string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Browser navigation: serve an HTML player page instead of raw audio.
	// Media players and <audio> elements send Accept: */*, not text/html.
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		repl := strings.NewReplacer("{{CLUBID}}", clubID, "{{CLUBNAME}}", h.clubName(clubID))
		if !h.mgr.isActive(clubID) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, repl.Replace(radioOfflinePage)) //nolint:errcheck
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		fmt.Fprint(w, repl.Replace(radioPlayerPage)) //nolint:errcheck
		return
	}

	if !h.mgr.isActive(clubID) {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	h.mgr.mu.Lock()
	rc := h.mgr.clubs[clubID]
	var station *radioStation
	if rc != nil {
		station = rc.station
	}
	h.mgr.mu.Unlock()

	if station == nil {
		http.Error(w, "radio not streaming", http.StatusServiceUnavailable)
		return
	}

	ch := station.subscribe()
	defer station.unsubscribe(ch)
	log.Printf("radio [%.8s] listener connected (total=%d)", clubID, station.listenerCount())

	wantICY := r.Header.Get("Icy-MetaData") == "1"
	clubName := h.clubName(clubID)

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // tell Caddy/nginx not to buffer the stream
	w.Header().Set("icy-name", clubName+" — zapclub.io")
	w.Header().Set("icy-description", "Live collaborative radio on zapclub.io")
	w.Header().Set("icy-artwork", icyArtwork)
	w.Header().Set("icy-url", icyStationURL)
	w.Header().Set("icy-br", "128")
	if wantICY {
		w.Header().Set("icy-metaint", fmt.Sprintf("%d", icyMetaint))
	}

	flusher, canFlush := w.(http.Flusher)
	sent := 0 // bytes written since last ICY metadata block

	writeWithICY := func(data []byte) error {
		if !wantICY {
			_, err := w.Write(data)
			return err
		}
		buf := &bytes.Buffer{}
		offset := 0
		for offset < len(data) {
			// How many audio bytes until the next metadata slot?
			room := icyMetaint - sent
			end := offset + room
			if end > len(data) {
				end = len(data)
			}
			buf.Write(data[offset:end])
			sent += end - offset
			offset = end
			if sent == icyMetaint {
				buf.Write(icyBlock(station.getTitle()))
				sent = 0
			}
		}
		_, err := w.Write(buf.Bytes())
		return err
	}

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			if err := writeWithICY(data); err != nil {
				log.Printf("radio [%.8s] listener write error: %v", clubID, err)
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			log.Printf("radio [%.8s] listener disconnected (remaining=%d)", clubID, station.listenerCount()-1)
			return
		}
	}
}

func (h *radioHandler) clubName(clubID string) string {
	if g, ok := h.cond.state.Groups.Load(clubID); ok {
		if name := g.Group.Name; name != "" {
			return name
		}
	}
	return clubID
}

func (h *radioHandler) handleM3U(w http.ResponseWriter, r *http.Request, clubID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	streamURL := fmt.Sprintf("%s://%s/radio/%s", scheme, r.Host, clubID)
	name := h.clubName(clubID)
	m3u := "#EXTM3U\n#EXTINF:-1," + name + " — zapclub.io Webradio\n" + streamURL + "\n"
	w.Header().Set("Content-Type", "audio/x-mpegurl")
	w.Header().Set("Content-Disposition", `attachment; filename="zapclub-radio.m3u"`)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	fmt.Fprint(w, m3u)
}

