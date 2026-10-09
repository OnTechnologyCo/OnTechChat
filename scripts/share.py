"""Single download entrypoint; serves only canonical output files."""
import contextlib, http.server, json, msvcrt, os, pathlib, re
import shutil, subprocess, threading, time
ROOT = pathlib.Path(__file__).resolve().parents[1]
STATE = ROOT / ".build"
STATE.mkdir(exist_ok=True)
lock = (STATE / "download.lock").open("a+b")
if lock.tell() == 0:
    lock.write(b"0")
    lock.flush()
lock.seek(0)
try:
    msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
except OSError:
    raise SystemExit("A download service is already running; read .build/download.json.")
FILES = {
    "/apk": (".apk", "application/vnd.android.package-archive"),
    "/server": (".exe", "application/octet-stream"),
}
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass
    def do_HEAD(self):
        self.serve(False)
    def do_GET(self):
        self.serve(True)
    def serve(self, body):
        path = self.path.split("?", 1)[0]
        if path == "/":
            data = ('<!doctype html><meta charset="utf-8"><title>OnTechChat</title>'
                    '<h1>OnTechChat</h1><p><a href="/apk">Android APK</a></p>'
                    '<p><a href="/server">Windows Server Setup</a></p>').encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            if body:
                self.wfile.write(data)
            return
        item = FILES.get(path)
        if not item:
            return self.send_error(404)
        try:
            manifest = json.loads((ROOT / "output" / "manifest.json").read_text(encoding="utf-8-sig"))
            names = [entry["File"] for entry in manifest if entry["File"].endswith(item[0])]
            if len(names) != 1 or pathlib.Path(names[0]).name != names[0]:
                return self.send_error(503, "Release manifest is not ready")
            filename = names[0]
            with (ROOT / "output" / filename).open("rb") as file:
                self.send_response(200)
                self.send_header("Content-Type", item[1])
                self.send_header("Content-Disposition", f'attachment; filename="{filename}"')
                self.send_header("Content-Length", str(os.fstat(file.fileno()).st_size))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                if body:
                    shutil.copyfileobj(file, self.wfile, 65536)
        except (FileNotFoundError, json.JSONDecodeError, KeyError, TypeError):
            self.send_error(503, "Build output is unavailable")
        except (BrokenPipeError, ConnectionResetError, TimeoutError):
            pass
server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
server.daemon_threads = True
threading.Thread(target=server.serve_forever, daemon=True).start()
log_path = STATE / "download-tunnel.log"
child = None
try:
    with log_path.open("w", encoding="utf-8") as log:
        child = subprocess.Popen([str(ROOT / "tools" / "cloudflared.exe"), "tunnel",
            "--no-autoupdate", "--url", f"http://127.0.0.1:{server.server_port}"],
            stdout=log, stderr=subprocess.STDOUT)
        url = ""
        for _ in range(60):
            if child.poll() is not None:
                raise RuntimeError("Download tunnel exited")
            match = re.search(r"https://[a-z0-9-]+\.trycloudflare\.com",
                              log_path.read_text(encoding="utf-8", errors="replace"))
            if match:
                url = match.group(0)
                break
            time.sleep(1)
        if not url:
            raise RuntimeError("Download URL not available")
        state = {"url": url, "apk": url + "/apk", "server": url + "/server",
                 "port": server.server_port, "pid": os.getpid(), "tunnelPid": child.pid,
                 "root": str(ROOT), "output": str(ROOT / "output")}
        (STATE / "download.json").write_text(json.dumps(state, indent=2), encoding="utf-8")
        print(url, flush=True)
        child.wait()
finally:
    server.shutdown()
    server.server_close()
    if child and child.poll() is None:
        child.terminate()
    lock.close()

