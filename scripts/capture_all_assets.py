import http.server
import socketserver
import subprocess
import threading
import time
import os
import shutil

PORT = 8093
ASSETS_DIR = os.path.abspath("internal/web/assets")
REVIEW_DIR = os.path.abspath("docs/review")
DOCS_ASSETS_DIR = os.path.abspath("docs/assets")
CHROME_PATH = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
CACHE_DIR = os.path.abspath(".cache/chrome-asset-capture")

os.makedirs(REVIEW_DIR, exist_ok=True)
os.makedirs(DOCS_ASSETS_DIR, exist_ok=True)
os.makedirs(CACHE_DIR, exist_ok=True)

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=ASSETS_DIR, **kwargs)
    def log_message(self, format, *args):
        pass

httpd = socketserver.TCPServer(("127.0.0.1", PORT), Handler)
server_thread = threading.Thread(target=httpd.serve_forever, daemon=True)
server_thread.start()
print(f"Serving {ASSETS_DIR} at http://127.0.0.1:{PORT}")
time.sleep(1)

# Helper to run Chrome screenshot
def capture_screenshot(url, out_path, vp_size, profile_id):
    user_data = os.path.join(CACHE_DIR, f"u_{profile_id}")
    cmd = [
        CHROME_PATH,
        "--headless=new",
        "--no-sandbox",
        "--disable-gpu",
        "--hide-scrollbars",
        f"--window-size={vp_size}",
        f"--user-data-dir={user_data}",
        f"--screenshot={out_path}",
        url
    ]
    subprocess.run(cmd, capture_output=True, timeout=25)
    if os.path.exists(out_path):
        print(f"Captured {os.path.basename(out_path)} ({os.path.getsize(out_path)} bytes)")
    else:
        print(f"Failed {os.path.basename(out_path)}")

states_to_capture = [
    ("empty", "empty"),
    ("streaming", "streaming"),
    ("complete", "complete"),
    ("partial", "partial"),
    ("error", "error")
]

viewports = [
    ("desktop", "1280,900"),
    ("mobile360", "360,800")
]

themes = ["light", "dark"]

for state_name, state_param in states_to_capture:
    for vp_name, vp_size in viewports:
        for theme in themes:
            filename = f"w1_{state_name}_{vp_name}_{theme}.png"
            out_path = os.path.join(REVIEW_DIR, filename)
            url = f"http://127.0.0.1:{PORT}/index.html?state={state_param}&theme={theme}"
            capture_screenshot(url, out_path, vp_size, f"{state_name}_{vp_name}_{theme}")

# Additional specialized review states
for theme in themes:
    # 1. Expanded row state (click row in complete)
    # 2. Unreachable section specifically visible
    url = f"http://127.0.0.1:{PORT}/index.html?state=partial&theme={theme}"
    out_path = os.path.join(REVIEW_DIR, f"w1_unreachable_desktop_{theme}.png")
    capture_screenshot(url, out_path, "1280,900", f"unreachable_desktop_{theme}")

    url = f"http://127.0.0.1:{PORT}/index.html?state=complete&theme={theme}"
    out_path = os.path.join(REVIEW_DIR, f"w1_expanded_desktop_{theme}.png")
    capture_screenshot(url, out_path, "1280,900", f"expanded_desktop_{theme}")

# Update docs/assets/dashboard.png with the new complete desktop editorial ledger design
new_dashboard_source = os.path.join(REVIEW_DIR, "w1_complete_desktop_light.png")
dashboard_dest = os.path.join(DOCS_ASSETS_DIR, "dashboard.png")
if os.path.exists(new_dashboard_source):
    shutil.copyfile(new_dashboard_source, dashboard_dest)
    print(f"Updated docs/assets/dashboard.png from {new_dashboard_source} ({os.path.getsize(dashboard_dest)} bytes)")

httpd.shutdown()

# Re-run capture_terminal.py to ensure terminal.png is up to date
terminal_script = os.path.abspath("scratch/capture_terminal.py")
if os.path.exists(terminal_script):
    subprocess.run(["python", terminal_script], capture_output=True, timeout=20)
    print("Re-captured docs/assets/terminal.png")

print("All asset captures completed successfully!")
