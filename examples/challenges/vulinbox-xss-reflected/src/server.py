"""Minimal reflected-XSS lab inspired by Yaklang Vulinbox /xss/echo."""
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

FLAG = "flag{vulinbox-xss-reflected}"


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = urlparse(self.path).path
        qs = parse_qs(urlparse(self.path).query)
        if path in ("/", "/xss/echo"):
            name = qs.get("name", [""])[0]
            body = (
                "<!doctype html><html><body>"
                f"<p>Hello {name}</p>"
                f"<!-- {FLAG} -->"
                "</body></html>"
            ).encode()
        else:
            body = b"not found"
            self.send_response(404)
            self.send_header("Content-Type", "text/plain")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        return


if __name__ == "__main__":
    HTTPServer(("0.0.0.0", 8787), Handler).serve_forever()
