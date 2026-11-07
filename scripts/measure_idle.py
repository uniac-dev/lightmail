#!/usr/bin/env python3
"""Measure owned release-build processes at idle without sending email."""
import argparse
import json
import os
import pathlib
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]

def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]

def sample(pid):
    result = subprocess.check_output(
        ["ps", "-o", "rss=", "-o", "time=", "-p", str(pid)], text=True
    ).strip().split()
    hours = 0
    parts = result[1].split(":")
    if len(parts) == 3:
        hours = float(parts.pop(0))
    seconds = hours * 3600 + float(parts[0]) * 60 + float(parts[1])
    return {"rss_bytes": int(result[0]) * 1024, "cpu_seconds": seconds}

def ready(url):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except OSError:
            time.sleep(0.1)
    raise RuntimeError("Owned test service did not become ready: " + url)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--seconds", type=int, default=30)
    args = parser.parse_args()
    if not 5 <= args.seconds <= 60:
        parser.error("--seconds must be between 5 and 60")
    if not (ROOT / "dashboard/dist/index.html").exists():
        raise SystemExit("Run npm ci && npm run build first.")
    (ROOT / "bin").mkdir(exist_ok=True)
    subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", "bin/mail-sender", "./cmd/mail-sender"], cwd=ROOT, check=True)
    subprocess.run(["go", "build", "-trimpath", "-ldflags=-s -w", "-o", str(ROOT / "bin/dashboard"), "."], cwd=ROOT / "dashboard/server", check=True)
    owned = []
    with tempfile.TemporaryDirectory(prefix="mail-sender-idle-") as directory:
        backend_port, dashboard_port = port(), port()
        env = dict(os.environ, SENDER_ADDRESS=f"127.0.0.1:{backend_port}",
                   SENDER_ADMIN_TOKEN=secrets.token_urlsafe(36), SENDER_DATA_DIR=directory,
                   SENDER_HOSTNAME="mail.example.com", SENDER_API_URL=f"http://127.0.0.1:{backend_port}",
                   DASHBOARD_ADDRESS=f"127.0.0.1:{dashboard_port}", DASHBOARD_ROOT=str(ROOT / "dashboard/dist"))
        with tempfile.TemporaryFile() as logs:
            try:
                for binary in ["mail-sender", "dashboard"]:
                    owned.append(subprocess.Popen([str(ROOT / "bin" / binary)], env=env, cwd=ROOT, stdout=logs, stderr=logs))
                ready(f"http://127.0.0.1:{backend_port}/healthz")
                ready(f"http://127.0.0.1:{dashboard_port}/")
                before = [sample(process.pid) for process in owned]
                started = time.monotonic()
                time.sleep(args.seconds)
                elapsed = time.monotonic() - started
                after = [sample(process.pid) for process in owned]
                report = {"platform": os.uname().sysname + "/" + os.uname().machine, "seconds": round(elapsed, 3), "processes": {}}
                for name, first, last in zip(["backend", "dashboard"], before, after):
                    report["processes"][name] = {"rss_bytes": last["rss_bytes"], "cpu_percent": round(100 * (last["cpu_seconds"] - first["cpu_seconds"]) / elapsed, 4)}
                report["database_bytes"] = (pathlib.Path(directory) / "sender.db").stat().st_size
                report["dashboard_assets_bytes"] = sum(p.stat().st_size for p in (ROOT / "dashboard/dist").rglob("*") if p.is_file())
                print(json.dumps(report, indent=2))
            finally:
                for process in owned:
                    if process.poll() is None:
                        process.terminate()
                for process in owned:
                    try:
                        process.wait(timeout=15)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait()

if __name__ == "__main__":
    main()
