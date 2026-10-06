#!/usr/bin/env python3
"""
Feature 22 — HTTP Backend Server for Real-Time AI Diagnostic Loop
Provides static file serving for web UI and POST /api/diagnose endpoint.
"""

import json
import os
import sys
import time
import threading
import uuid
from http.server import ThreadingHTTPServer, SimpleHTTPRequestHandler
import urllib.parse

# Ensure project root in python path
PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
if PROJECT_ROOT not in sys.path:
    sys.path.insert(0, PROJECT_ROOT)

from runtime.diagnostic_loop import NeedleDiagnoser, ToolDispatcherBridge

# Global diagnoser instance loaded once at server startup
GLOBAL_DIAGNOSER = None
CHECKPOINT_PATH = os.path.join(PROJECT_ROOT, "checkpoints", "needle3.safetensors")

# Background job registry
JOBS = {}
JOBS_LOCK = threading.Lock()


class DiagnosticHandler(SimpleHTTPRequestHandler):

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=os.path.join(PROJECT_ROOT, "web"), **kwargs)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/api/diagnose/status":
            self.handle_api_diagnose_status(parsed.query)
        else:
            super().do_GET()

    def handle_api_diagnose_status(self, query_str: str):
        qs = urllib.parse.parse_qs(query_str)
        job_id = qs.get("job_id", [""])[0]

        with JOBS_LOCK:
            job = JOBS.get(job_id)
            if job:
                # Return snapshot of job state
                snapshot = dict(job)

        if not job:
            self._send_json({"error": f"Diagnostic job ID '{job_id}' not found"}, status=404)
            return

        self._send_json(snapshot, status=200)

    def do_POST(self):
        if self.path.startswith("/api/diagnose"):
            self.handle_api_diagnose()
        else:
            self.send_error(404, "Endpoint Not Found")

    def handle_api_diagnose(self):
        content_length = int(self.headers.get("Content-Length", 0))
        post_data = self.rfile.read(content_length)

        try:
            req_data = json.loads(post_data.decode("utf-8"))
        except json.JSONDecodeError:
            self._send_json({"error": "Invalid JSON request payload"}, status=400)
            return

        namespace = str(req_data.get("namespace", "default")).strip()
        pod = str(req_data.get("pod", "")).strip()
        question = str(req_data.get("question", "")).strip()
        provider_type = str(req_data.get("provider", "local-kubernetes")).strip()
        kubeconfig_content = req_data.get("kubeconfig")
        if kubeconfig_content is not None:
            kubeconfig_content = str(kubeconfig_content).strip() or None

        # Validation
        if not question:
            if pod:
                question = f"Why is pod {pod} failing in namespace {namespace}?"
            else:
                self._send_json({"error": "Question or Pod name is required"}, status=400)
                return

        if not namespace:
            namespace = "default"

        if GLOBAL_DIAGNOSER is None:
            self._send_json({"error": "Needle 3 model checkpoint not initialized"}, status=500)
            return

        job_id = uuid.uuid4().hex
        print(f"\n[API] POST /api/diagnose | job_id={job_id} namespace={namespace} pod={pod} provider={provider_type}", flush=True)
        print(f"[API] Question: {question}", flush=True)

        bridge = ToolDispatcherBridge(provider_type=provider_type, project_dir=PROJECT_ROOT, kubeconfig_content=kubeconfig_content)

        job_entry = {
            "job_id": job_id,
            "status": "running",
            "created_at": time.time(),
            "question": question,
            "namespace": namespace,
            "pod": pod,
            "model": "Needle 3 — F19",
            "provider": provider_type,
            "elapsed_seconds": 0,
            "iterations": 1,
            "steps": [],
            "diagnosis": {},
            "final_diagnosis_text": "",
            "raw_response": "",
            "evidence_collected": [],
            "error": None,
        }

        with JOBS_LOCK:
            JOBS[job_id] = job_entry

        def run_job_bg():
            t0 = time.time()
            try:
                def on_step(step):
                    with JOBS_LOCK:
                        if job_id in JOBS:
                            JOBS[job_id]["steps"].append(step)

                res = GLOBAL_DIAGNOSER.diagnose(
                    question,
                    bridge=bridge,
                    verbose=True,
                    step_callback=on_step,
                    namespace=namespace,
                    pod=pod,
                )
                elapsed = round(time.time() - t0, 2)
                with JOBS_LOCK:
                    if job_id in JOBS:
                        j = JOBS[job_id]
                        j["status"] = "completed"
                        j["elapsed_seconds"] = elapsed
                        j["iterations"] = res.get("iterations", 1)
                        j["diagnosis"] = res.get("structured_diagnosis", {})
                        j["final_diagnosis_text"] = res.get("final_diagnosis", "")
                        j["raw_response"] = res.get("raw_response", "")
                        j["steps"] = res.get("steps", [])
                        j["evidence_collected"] = res.get("evidence_collected", [])
            except Exception as e:
                print(f"[API ERROR] Job {job_id} failed: {e}", flush=True)
                import traceback
                traceback.print_exc()
                with JOBS_LOCK:
                    if job_id in JOBS:
                        JOBS[job_id]["status"] = "error"
                        JOBS[job_id]["error"] = f"Diagnostic loop processing error: {str(e)}"
                        JOBS[job_id]["elapsed_seconds"] = round(time.time() - t0, 2)

        t = threading.Thread(target=run_job_bg, daemon=True)
        t.start()

        # Send immediate async acknowledgment
        self._send_json({
            "job_id": job_id,
            "status": "running",
            "question": question,
            "namespace": namespace,
            "pod": pod,
            "provider": provider_type,
        }, status=200)

    def _send_json(self, data: dict, status: int = 200):
        body = json.dumps(data, indent=2, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def do_OPTIONS(self):
        self.send_response(200)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.end_headers()


def run_server(port: int = 8080):
    global GLOBAL_DIAGNOSER

    if not os.path.exists(CHECKPOINT_PATH):
        print(f"ERROR: Needle 3 F19 checkpoint not found at {CHECKPOINT_PATH}", file=sys.stderr)
        sys.exit(1)

    abs_path = os.path.abspath(CHECKPOINT_PATH)
    file_size = os.path.getsize(abs_path)

    print("==========================================================")
    print("  stack8s-ai-diagnose — Real-Time AI Observability Demo")
    print("==========================================================")
    print(f"Loaded Needle 3 F19 checkpoint: checkpoints/needle3.safetensors")
    print(f"  Absolute Path : {abs_path}")
    print(f"  File Size     : {file_size} bytes ({file_size / (1024 * 1024):.2f} MB)")
    print("  Initializing Needle 3 F19 model into memory...")
    
    t0 = time.time()
    GLOBAL_DIAGNOSER = NeedleDiagnoser(CHECKPOINT_PATH)
    print(f"  Model loaded in {time.time()-t0:.2f}s!")

    # Verify the configured Kubernetes provider target before serving traffic.
    _provider_type = os.environ.get("STACK8S_PROVIDER", "local-kubernetes")
    try:
        _probe = ToolDispatcherBridge(provider_type=_provider_type, project_dir=PROJECT_ROOT)
        print(f"  Provider target check: {_probe.describe_provider()}")
    except Exception as _e:
        print(f"  Provider target check failed: {_e}")

    print(f"  Serving Web UI & API at: http://localhost:{port}")
    print("==========================================================")

    server_address = ("", port)
    ThreadingHTTPServer.allow_reuse_address = True
    httpd = ThreadingHTTPServer(server_address, DiagnosticHandler)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down server...")
        httpd.server_close()


if __name__ == "__main__":
    port = 8080
    if len(sys.argv) > 1 and sys.argv[1].isdigit():
        port = int(sys.argv[1])
    run_server(port)
