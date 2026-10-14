"""SQL-injection login-bypass lab with an in-container checkpoint checker.

Environment injected by the platform on instance start:
  FLAG               unique per-instance flag (flag_mode=instance)
  CHECKPOINT_TOKEN   capability token for the auto-judge callback
  CHECKPOINT_URL     POST this URL when the goal is achieved

Flow: the login handler concatenates the username into a SQL-ish query.
A successful bypass (query condition true / password check skipped) is
detected server-side, the checker fires ONE checkpoint callback, and the
platform auto-judges the challenge — no manual flag submission needed.
"""
import json
import os
import urllib.request
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs, urlparse

FLAG = os.environ.get("FLAG", "flag{unset-start-via-platform}")
CHECKPOINT_URL = os.environ.get("CHECKPOINT_URL", "")
CHECKPOINT_TOKEN = os.environ.get("CHECKPOINT_TOKEN", "")

# 万能密码绕过的判定：用户名注入使条件恒真（含 or + 引号闭合），
# 且密码校验被注释/短路（-- 或 # 或空密码恒真）。宽松匹配常见载荷形态。
BYPASS_PATTERN = ("'", "or")

solved = False


def looks_like_bypass(username: str) -> bool:
    low = username.lower()
    if "'" not in low:
        return False
    return ("or" in low and "--" in low) or ("or '1'='1" in low) or ("or 1=1" in low)


def fire_checkpoint():
    """Notify the platform that the lab goal is achieved (auto-judge)."""
    global solved
    if solved or not CHECKPOINT_URL:
        return
    solved = True
    try:
        req = urllib.request.Request(CHECKPOINT_URL, data=b"", method="POST")
        urllib.request.urlopen(req, timeout=5)
    except Exception:
        solved = False  # 网络失败可重试（下次成功登录再回调）


class Handler(BaseHTTPRequestHandler):
    def _send(self, code: int, body: str, ctype: str = "text/html; charset=utf-8"):
        data = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        path = urlparse(self.path).path
        if path in ("/", "/login"):
            self._send(200, f"""
<!doctype html><html><body style="font-family: sans-serif; max-width: 420px; margin: 4rem auto">
  <h2>Member Login</h2>
  <p>演示登录：admin / S3cretP@ss（不可见明文，仅模拟）</p>
  <form method="POST" action="/login">
    <p><input name="username" placeholder="username" style="width:100%;padding:8px"></p>
    <p><input name="password" type="password" placeholder="password" style="width:100%;padding:8px"></p>
    <p><button style="padding:8px 16px">Sign in</button></p>
  </form>
  <!-- {FLAG} -->
</body></html>""")
        else:
            self._send(404, "not found", "text/plain")

    def do_POST(self):
        if urlparse(self.path).path != "/login":
            self._send(404, "not found", "text/plain")
            return
        length = int(self.headers.get("Content-Length", 0))
        form = parse_qs(self.rfile.read(length).decode())
        username = form.get("username", [""])[0]
        password = form.get("password", [""])[0]

        # 漏洞点：字符串直拼进查询条件（模拟后端 SQL 拼接）。
        query_ok = looks_like_bypass(username) or (username == "admin" and password == "S3cretP@ss")

        if query_ok:
            fire_checkpoint()  # 服务端确认目标达成 → 平台自动判定通关
            self._send(200, f"""
<!doctype html><html><body style="font-family: sans-serif; max-width: 420px; margin: 4rem auto">
  <h2 style="color:green">Welcome, admin!</h2>
  <p>登录绕过成功，平台已自动判定通关。</p>
  <p>flag（备用手动提交）：{FLAG}</p>
</body></html>""")
        else:
            self._send(401, """
<!doctype html><html><body style="font-family: sans-serif; max-width: 420px; margin: 4rem auto">
  <h2 style="color:red">Login failed</h2>
  <p>用户名或密码错误。</p>
</body></html>""")

    def log_message(self, *_args):
        return


if __name__ == "__main__":
    print(f"lab listening on :8080 (checkpoint {'on' if CHECKPOINT_URL else 'off'})")
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()