"""
zapclub.io LNURL + NIP-05 proxy.

Serves /.well-known/lnurlp/{user} and /.well-known/nostr.json by proxying
to FALLBACK_DOMAIN (default: nsnip.io). Rewrites text/identifier to
user@zapclub.io so wallets display the right address.
"""
import json
import os
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

FALLBACK = os.getenv("LNURL_FALLBACK_DOMAIN", "nsnip.io")
OUR_DOMAIN = os.getenv("LNURL_DOMAIN", "zapclub.io")
RELAY_PUBLIC_URL = os.getenv("RELAY_PUBLIC_URL", "wss://relay.zapclub.io")
PORT = int(os.getenv("PORT", "3335"))


def _fetch(url: str) -> dict | None:
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "zapclub-lnurlp-proxy/1.0"})
        with urllib.request.urlopen(req, timeout=8) as r:
            return json.loads(r.read())
    except Exception:
        return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        print(f"[lnurlp] {self.address_string()} {fmt % args}")

    def _send_json(self, data: dict, status: int = 200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path

        # /.well-known/lnurlp/{user}
        if path.startswith("/.well-known/lnurlp/"):
            user = path.split("/")[-1].lower().strip()
            if not user:
                self._send_json({"status": "ERROR", "reason": "missing user"}, 400)
                return
            data = _fetch(f"https://{FALLBACK}/.well-known/lnurlp/{user}")
            if not data:
                self._send_json({"status": "ERROR", "reason": "user not found"}, 404)
                return
            # Rewrite text/identifier so wallets show user@zapclub.io
            try:
                meta = json.loads(data.get("metadata", "[]"))
                data["metadata"] = json.dumps([
                    [t, f"{user}@{OUR_DOMAIN}"] if t == "text/identifier" else [t, v]
                    for t, v in meta
                ])
            except Exception:
                pass
            self._send_json(data)
            return

        # /.well-known/nostr.json?name={user}
        if path == "/.well-known/nostr.json":
            qs = parse_qs(parsed.query)
            name = (qs.get("name") or [""])[0].lower().strip()
            url = f"https://{FALLBACK}/.well-known/nostr.json"
            if name:
                url += f"?name={name}"
            data = _fetch(url)
            if not data:
                self._send_json({"names": {}, "relays": {}})
                return
            names = data.get("names") or {}
            if name:
                names = {name: names[name]} if name in names else {}
            # Point relays to our relay
            relays = {pk: [RELAY_PUBLIC_URL] for pk in names.values()}
            self._send_json({"names": names, "relays": relays})
            return

        self._send_json({"status": "ERROR", "reason": "not found"}, 404)


if __name__ == "__main__":
    server = HTTPServer(("127.0.0.1", PORT), Handler)
    print(f"lnurlp proxy → {FALLBACK}, domain={OUR_DOMAIN}, port={PORT}")
    server.serve_forever()
