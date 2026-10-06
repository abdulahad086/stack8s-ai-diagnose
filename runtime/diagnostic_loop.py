#!/usr/bin/env python3
"""
Feature 22 — Real-Time Diagnostic Loop (MVP Runtime Integration)
Integration module between Needle 3 model (F19 validated checkpoint) and Go Provider tool dispatcher.
"""

import json
import os
import re
import subprocess
import sys
import time

VALID_TOOLS = {
    "get_pod_status": ["namespace", "pod"],
    "get_pod_events": ["namespace", "pod"],
    "get_container_logs": ["namespace", "pod", "container"],
    "get_node_status": ["node_name"],
    "get_gpu_status": ["node_name"],
    "get_service_status": ["namespace"],
    "get_storage_status": ["namespace"],
    "get_workload_status": ["namespace"],
    "get_configuration_status": ["namespace", "pod"],
}


def parse_tool_calls(raw_text: str) -> list:
    """Extract and parse <tool_call>[...]</tool_call> tags from Needle output."""
    raw_clean = re.sub(r"<think>.*?</think>", "", raw_text, flags=re.DOTALL)
    calls = []
    for match in re.findall(r"<tool_call>(.*?)</tool_call>", raw_clean, re.DOTALL):
        match_str = match.strip()
        if not match_str:
            continue
        try:
            parsed = json.loads(match_str)
            if isinstance(parsed, list):
                calls.extend(parsed)
            elif isinstance(parsed, dict):
                calls.append(parsed)
        except json.JSONDecodeError:
            calls.append({"_invalid": True, "_error": "JSON syntax error", "_raw": match_str[:200]})
    return calls


def validate_tool_call(call: dict) -> tuple[bool, str]:
    """Validate tool name, required parameters, and non-empty values."""
    if not isinstance(call, dict) or call.get("_invalid"):
        return False, call.get("_error", "Invalid tool call format")
    name = call.get("name")
    if not name or name not in VALID_TOOLS:
        return False, f"Unknown tool name: '{name}'"
    args = call.get("arguments")
    if not isinstance(args, dict):
        return False, "Arguments must be a JSON object"
    required = VALID_TOOLS[name]
    for req in required:
        if req not in args or args[req] is None or str(args[req]).strip() == "":
            return False, f"Tool '{name}' missing required argument '{req}'"
    return True, ""


class ToolDispatcherBridge:
    """Executes provider tools via Go Provider ToolDispatcher."""

    def __init__(self, provider_type: str = "local-kubernetes", project_dir: str = None, kubeconfig_content: str = None):
        self.provider_type = provider_type
        self.project_dir = project_dir or os.getcwd()
        self.kubeconfig_content = kubeconfig_content

    def _write_kubeconfig(self):
        import tempfile
        if not self.kubeconfig_content:
            return None
        fd, path = tempfile.mkstemp(prefix="kubeconfig-", suffix=".yaml")
        with os.fdopen(fd, "w") as f:
            f.write(self.kubeconfig_content)
        return path

    def execute_tool(self, tool_name: str, arguments: dict) -> dict:
        env = os.environ.copy()
        env["STACK8S_PROVIDER"] = self.provider_type
        kube_path = self._write_kubeconfig()
        if kube_path:
            env["STACK8S_KUBECONFIG_PATH"] = kube_path
            env["KUBECONFIG"] = kube_path
        cmd = [
            "go", "run", "./cmd/tool_exec",
            "--tool", tool_name,
            "--args", json.dumps(arguments),
        ]
        try:
            res = subprocess.run(
                cmd,
                cwd=self.project_dir,
                env=env,
                capture_output=True,
                text=True,
                timeout=30,
            )
            if res.returncode != 0 and not res.stdout.strip():
                return {
                    "tool": tool_name,
                    "arguments": arguments,
                    "error": f"Go dispatcher exit {res.returncode}: {res.stderr.strip()}",
                }
            # Surface dispatcher diagnostics (provider target) without leaking secrets.
            if res.stderr.strip():
                print(f"  [dispatcher] {res.stderr.strip().splitlines()[-1]}", flush=True)
            if kube_path and os.path.exists(kube_path):
                os.unlink(kube_path)
            return json.loads(res.stdout.strip())
        except Exception as e:
            if kube_path and os.path.exists(kube_path):
                os.unlink(kube_path)
            return {
                "tool": tool_name,
                "arguments": arguments,
                "error": f"Tool execution failed: {str(e)}",
            }

    def describe_provider(self) -> dict:
        """Return the provider's configured name and cluster host (no credentials)."""
        env = os.environ.copy()
        env["STACK8S_PROVIDER"] = self.provider_type
        kube_path = self._write_kubeconfig()
        if kube_path:
            env["STACK8S_KUBECONFIG_PATH"] = kube_path
            env["KUBECONFIG"] = kube_path
        cmd = ["go", "run", "./cmd/tool_exec", "--print-target"]
        res = None
        try:
            res = subprocess.run(
                cmd, cwd=self.project_dir, env=env,
                capture_output=True, text=True, timeout=30,
            )
            return json.loads(res.stdout.strip())
        except Exception as e:
            return {"error": f"Unable to determine provider target: {e}", "stderr": (res.stderr if res else "")}
        finally:
            if kube_path and os.path.exists(kube_path):
                os.unlink(kube_path)


def _is_resource_not_found(evidence: dict) -> bool:
    err = str(evidence.get("error", "") or "")
    res = evidence.get("result")
    if isinstance(res, dict):
        err += " " + str(res.get("error", "") or "") + " " + str(res.get("code", "") or "")
    return "RESOURCE_NOT_FOUND" in err or "not found" in err.lower()


class NeedleDiagnoser:
    """Diagnostic loop coordinator for Needle 3 F19 model."""

    def __init__(self, checkpoint_path: str = "checkpoints/needle3.safetensors", tools_file: str = "dataset/tools.json"):
        from needle.model.run import load_checkpoint, get_tokenizer, SimpleAttentionNetwork

        if not os.path.exists(checkpoint_path):
            raise FileNotFoundError(f"Needle 3 F19 checkpoint not found at: {checkpoint_path}")

        self.abs_path = os.path.abspath(checkpoint_path)
        self.file_size = os.path.getsize(self.abs_path)

        print(f"Loaded Needle 3 F19 checkpoint: checkpoints/needle3.safetensors", flush=True)
        print(f"  Absolute Path : {self.abs_path}", flush=True)
        print(f"  File Size     : {self.file_size} bytes ({self.file_size / (1024 * 1024):.2f} MB)", flush=True)

        t0 = time.time()
        self.params, self.config = load_checkpoint(self.abs_path)
        self.model = SimpleAttentionNetwork(self.config)
        self.tokenizer = get_tokenizer(self.config.vocab_size)
        print(f"Needle 3 F19 model loaded into memory in {time.time()-t0:.2f}s", flush=True)

        with open(tools_file) as f:
            self.tools_schema = json.load(f)

        # Use a practical fixed buffer length that fits the tool-schema prompt + short output.
        # CRITICAL: this must be the SAME value in warmup and all generate_step calls
        # so the JAX JIT-compiled decode_fn is always reused from cache (no re-compilation).
        # 8192 (max_seq_len) would take 10+ min to JIT on CPU — 1280 fits comfortably.
        self._fixed_buf_len = 1280

        print("  Warming up JAX inference engine...", flush=True)
        tw = time.time()
        try:
            from needle.model.run import build_prompt
            warmup_p = build_prompt("Why is pod test-pod failing in namespace default?", self.tools_schema)
            self.generate_step(warmup_p, max_tokens=1)
            print(f"  JAX inference engine warmed up in {time.time()-tw:.2f}s", flush=True)
        except Exception as ex:
            print(f"  JAX warmup skipped: {ex}", flush=True)

    def generate_step(self, prompt: str, max_tokens: int = 60, seed: int = 42) -> str:
        """Generate up to max_tokens new tokens, stopping early on </tool_call> or <|im_end|>.
        Uses a FIXED buffer length (model.config.max_seq_len) so the JAX JIT-compiled
        decode_fn is always reused from the warmup cache — no re-compilation per request.
        """
        from needle.model.run import _get_decode_fn, BOS_ID, EOS_ID, PAD_ID
        import jax.numpy as jnp

        prompt_ids = [BOS_ID] + self.tokenizer.encode(prompt)

        # Always use the fixed buf_len so JIT cache is always hit
        buf_len = getattr(self, '_fixed_buf_len', self.model.config.max_seq_len)

        if len(prompt_ids) >= buf_len:
            # Prompt is too long — truncate from the left keeping the last buf_len tokens
            prompt_ids = prompt_ids[-(buf_len - 1):]

        buffer = jnp.full((1, buf_len), PAD_ID, dtype=jnp.int32)
        buffer = buffer.at[0, :len(prompt_ids)].set(jnp.array(prompt_ids, dtype=jnp.int32))
        decode_fn = _get_decode_fn(self.model, buf_len)

        generated = []
        max_gen_pos = min(buf_len - 1, len(prompt_ids) - 1 + max_tokens)
        for pos in range(len(prompt_ids) - 1, max_gen_pos):
            logits = decode_fn(self.params, buffer)[0, pos]
            next_token = int(jnp.argmax(logits))
            if next_token == EOS_ID:
                break
            generated.append(next_token)
            buffer = buffer.at[0, pos + 1].set(next_token)

            text = self.tokenizer.decode(generated)
            if "</tool_call>" in text or "<|im_end|>" in text:
                break

        return self.tokenizer.decode(generated)

    def diagnose(
        self,
        query: str,
        bridge: ToolDispatcherBridge,
        max_iterations: int = 5,
        verbose: bool = True,
        step_callback: callable = None,
        namespace: str = None,
        pod: str = None,
    ) -> dict:
        """
        Hybrid diagnostic loop:
        1. Deterministically extract pod/namespace from query (instant)
        2. Run the most relevant K8s tools via Go provider (fast, ~1s each)
        3. Run Needle 3 ONCE on gathered evidence for final diagnosis (1-2 min)
        This reduces total time from 5-10 min to 1-2 min on CPU.
        """
        from needle.model.run import build_prompt

        evidence_collected = []
        steps = []

        def record_step(step_data):
            steps.append(step_data)
            if step_callback:
                try:
                    step_callback(step_data)
                except Exception:
                    pass

        # --- Phase 1: Deterministic tool selection from query (instant) ---
        # Explicitly supplied namespace/pod (from UI/API) ALWAYS take precedence
        # over anything extracted from free-form question text.
        extracted_pod, extracted_ns = self._extract_pod_namespace(query)
        pod_name = pod if pod else extracted_pod
        namespace = namespace if namespace else extracted_ns
        planned_calls, node_intent = self._plan_tool_calls(query, pod_name, namespace)

        if verbose:
            print(f"\n[Phase 1] Target: namespace={namespace!r} pod={pod_name!r}", flush=True)
            print(f"[Phase 1] Deterministic tool plan: {[c['name'] for c in planned_calls]}", flush=True)

        # --- Phase 2: Execute tools via Go provider (fast) ---
        executed_calls = set()
        discovered_containers = []

        i = 0
        while i < len(planned_calls):
            call = planned_calls[i]
            i += 1

            tool_name = call["name"]
            args = dict(call["arguments"])

            # Never request container logs when the target pod is still Pending
            # and unscheduled — its containers have never started, so logs
            # cannot contain useful evidence.
            if tool_name == "get_container_logs" and self._pod_not_started(evidence_collected, pod_name):
                record_step({
                    "type": "skipped",
                    "tool": tool_name,
                    "arguments": args,
                    "reason": "pod is Pending/unscheduled; containers have not started, logs cannot exist",
                })
                if verbose:
                    print(f"  [SKIP] {tool_name} skipped: pod is Pending/unscheduled (no container could have produced logs)", flush=True)
                continue

            # If real container name(s) were discovered from get_pod_status, update get_container_logs
            if tool_name == "get_container_logs" and discovered_containers:
                current_c = str(args.get("container", "")).strip()
                if not current_c or current_c == pod_name:
                    args["container"] = discovered_containers[0]

            call_key = (tool_name, json.dumps(args, sort_keys=True))
            if call_key in executed_calls:
                continue
            executed_calls.add(call_key)

            record_step({
                "type": "tool_call",
                "tool": tool_name,
                "arguments": args,
            })

            if verbose:
                print(f"  [EXECUTING TOOL] {tool_name}({args}) via Go Provider ({bridge.provider_type})...", flush=True)

            evidence = bridge.execute_tool(tool_name, args)
            evidence_collected.append(evidence)

            record_step({
                "type": "evidence",
                "tool": tool_name,
                "arguments": args,
                "result": evidence.get("result"),
                "error": evidence.get("error"),
            })

            # A RESOURCE_NOT_FOUND on the target resource is terminal:
            # stop the investigation instead of collecting dependent tools'
            # noise and never let it become a fabricated root cause.
            if _is_resource_not_found(evidence):
                for remaining in planned_calls[i:]:
                    record_step({
                        "type": "skipped",
                        "tool": remaining["name"],
                        "arguments": remaining["arguments"],
                        "reason": "target resource not found; investigation stopped",
                    })
                if verbose:
                    print(f"  [STOP] {tool_name} reported RESOURCE_NOT_FOUND; skipping remaining tools", flush=True)
                break

            # Extract discovered container names from get_pod_status result
            if tool_name == "get_pod_status" and isinstance(evidence.get("result"), dict):
                res = evidence["result"]
                containers = res.get("containers", [])
                if isinstance(containers, list):
                    for c in containers:
                        if isinstance(c, dict) and c.get("name"):
                            c_name = str(c["name"]).strip()
                            if c_name and c_name not in discovered_containers:
                                discovered_containers.append(c_name)

                    # If extra containers were discovered, queue log calls for them
                    if len(discovered_containers) > 1:
                        for extra_c in discovered_containers[1:]:
                            extra_call = {
                                "name": "get_container_logs",
                                "arguments": {"namespace": namespace, "pod": pod_name, "container": extra_c},
                            }
                            if extra_call not in planned_calls:
                                planned_calls.append(extra_call)

                # Node/GPU health questions: discover node_name and query it.
                if node_intent:
                    discovered_node = str(res.get("node_name", "")).strip()
                    if discovered_node and discovered_node not in ("<not yet scheduled>", "<none>"):
                        is_gpu = "gpu" in query.lower()
                        followup = {
                            "name": "get_gpu_status" if is_gpu else "get_node_status",
                            "arguments": {"node_name": discovered_node},
                        }
                        if followup not in planned_calls:
                            planned_calls.append(followup)

        # --- Phase 3: Run Needle 3 ONCE with all evidence for final synthesis ---
        if verbose:
            print(f"\n--- [Phase 3] Querying Needle 3 for final diagnosis synthesis ---", flush=True)

        # Verify the actual provider target BEFORE involving Needle, so we never
        # silently diagnose the wrong cluster (e.g. localhost fallback).
        describe_fn = getattr(bridge, "describe_provider", None)
        if callable(describe_fn):
            target_info = describe_fn()
            if verbose:
                print(f"[Provider target] {target_info}", flush=True)

        evidence_summary = json.dumps(evidence_collected, indent=2, ensure_ascii=False)[:2000]
        target_block = f"Target namespace: {namespace}\n"
        if pod_name:
            target_block += f"Target pod: {pod_name}\n"
        synthesis_query = (
            f"{query}\n\n{target_block}\nKubernetes Evidence Collected:\n{evidence_summary}\n\n"
            "Based on the evidence above, what is the root cause and recommended fix?"
        )
        conversation_prompt = build_prompt(synthesis_query, self.tools_schema)
        raw_response = self.generate_step(conversation_prompt)

        if verbose:
            print("Needle 3 synthesis complete.", flush=True)

        final_text, diag_dict = self._format_final_diagnosis(raw_response, evidence_collected, query)

        return {
            "query": query,
            "iterations": 1,
            "steps": steps,
            "evidence_collected": evidence_collected,
            "raw_response": raw_response,
            "final_diagnosis": final_text,
            "structured_diagnosis": diag_dict,
        }

    def _first_container_wait(self, evidence: list, pod_name: str) -> tuple[str, str]:
        """First Waiting container reason/message from pod-status evidence, e.g.
        ('CreateContainerConfigError', 'configmap "x" not found')."""
        for it in evidence:
            if it.get("tool") != "get_pod_status":
                continue
            res = it.get("result")
            if not isinstance(res, dict):
                continue
            for c in (res.get("containers", []) or []) + (res.get("init_containers", []) or []):
                if not isinstance(c, dict):
                    continue
                m = re.match(r"\s*Waiting \(([^:)]+)(?:: (.*))?\)", str(c.get("state", "")))
                if m:
                    return m.group(1), (m.group(2) or "")
        return "", ""

    @staticmethod
    def _is_resource_not_found(evidence: dict) -> bool:
        return _is_resource_not_found(evidence)

    def _pod_not_started(self, evidence_collected: list, pod_name: str) -> bool:
        """True when collected get_pod_status shows the pod has never run:
        phase Pending, no node assigned, and no container in Running state or
        with a last-termination record (i.e. it may have logged something)."""
        for it in evidence_collected:
            if it.get("tool") != "get_pod_status":
                continue
            res = it.get("result")
            if not isinstance(res, dict):
                continue
            if pod_name and res.get("pod_name") not in (None, pod_name):
                continue
            phase = str(res.get("phase", ""))
            node = str(res.get("node_name", "") or "")
            containers = res.get("containers", []) or []
            any_running = any(
                str(c.get("state", "")).startswith("Running") for c in containers if isinstance(c, dict)
            )
            any_terminated = any(
                c.get("has_last_termination") for c in containers if isinstance(c, dict)
            )
            if phase == "Pending" and not any_running and not any_terminated:
                return True
        return False

    def _extract_pod_namespace(self, query: str) -> tuple[str, str]:
        """Extract pod name and namespace from natural language query."""
        query_lower = query.lower()

        # Try to find "pod <name> " pattern
        pod_match = re.search(r"pod\s+([\w\-\.]+)", query, re.IGNORECASE)
        pod_name = pod_match.group(1) if pod_match else ""

        # Try to find "namespace <name>" or "in namespace <name>"
        ns_match = re.search(r"(?:in\s+)?namespace\s+([\w\-\.]+)", query, re.IGNORECASE)
        namespace = ns_match.group(1) if ns_match else "default"

        return pod_name, namespace

    def _plan_tool_calls(self, query: str, pod_name: str, namespace: str) -> tuple[list, bool]:
        """Deterministically select tools based on query keywords and pod/namespace.

        Returns (planned_calls, node_intent). node_intent=True means the user is
        asking about the node's health/resources, so pod events/logs are skipped
        and a follow-up get_node_status call is queued (using the node name
        discovered from get_pod_status) during Phase 2.
        """
        query_lower = query.lower()
        calls = []

        # Generic domain-intent detection. The user's question determines which
        # domain(s) matter; the loop must not default to CrashLoop investigation.
        service_intent = any(kw in query_lower for kw in ["service", "network", "endpoint", "ingress"])
        storage_intent = any(kw in query_lower for kw in ["storage", "pvc", "volume", "persistent"])
        config_intent = any(kw in query_lower for kw in ["config", "configmap", "secret", "identity", "serviceaccount", "service account", "env", "mount"])
        workload_intent = any(kw in query_lower for kw in ["workload", "deployment", "replica", "scale", "healthy", "not becoming healthy"])
        gpu_intent = "gpu" in query_lower
        node_intent = any(kw in query_lower for kw in [
            "node status", "node health", "node resource", "node's",
            "health of the node", "resource status of the node",
            "node cpu", "node memory", "node capacity", "node allocatable",
            "worker node", "what node", "which node",
        ]) or ("node" in query_lower and any(k in query_lower for k in ["health", "resource", "status", "cpu", "memory", "capacity", "allocatable"]))

        # Start with pod status whenever we have a pod target — this is also how
        # we discover the node name dynamically.
        if pod_name:
            calls.append({
                "name": "get_pod_status",
                "arguments": {"namespace": namespace, "pod": pod_name},
            })

        if node_intent or gpu_intent:
            # Node/GPU health questions: events/logs are skipped; the follow-up
            # node tool call is queued in Phase 2 after node_name discovery.
            return calls, True

        if storage_intent:
            wl_args = {"namespace": namespace}
            if pod_name:
                wl_args["pod"] = pod_name
            calls.append({"name": "get_storage_status", "arguments": wl_args})
            return calls, False

        if service_intent:
            calls.append({"name": "get_service_status", "arguments": {"namespace": namespace}})
            return calls, False

        if config_intent:
            cfg_args = {"namespace": namespace}
            if pod_name:
                cfg_args["pod"] = pod_name
            calls.append({"name": "get_configuration_status", "arguments": cfg_args})
            return calls, False

        if workload_intent:
            wl_args = {"namespace": namespace}
            if pod_name:
                wl_args["pod"] = pod_name
            calls.append({"name": "get_workload_status", "arguments": wl_args})
            return calls, False

        if pod_name:
            calls.append({
                "name": "get_pod_events",
                "arguments": {"namespace": namespace, "pod": pod_name},
            })

        # Add logs for crash/error patterns
        if any(kw in query_lower for kw in ["log", "crash", "error", "oom", "killed", "fail", "why", "reason"]):
            if pod_name:
                calls.append({
                    "name": "get_container_logs",
                    "arguments": {"namespace": namespace, "pod": pod_name, "container": pod_name},
                })

        # Add workload status for general queries
        if any(kw in query_lower for kw in ["workload", "deployment", "replica", "scale", "healthy", "not becoming healthy"]):
            wl_args = {"namespace": namespace}
            if pod_name:
                wl_args["pod"] = pod_name
            calls.append({
                "name": "get_workload_status",
                "arguments": wl_args,
            })

        # Add configuration status for config issues
        if any(kw in query_lower for kw in ["config", "configmap", "secret", "env", "mount", "volume"]):
            if pod_name:
                calls.append({
                    "name": "get_configuration_status",
                    "arguments": {"namespace": namespace, "pod": pod_name},
                })

        # Fallback: if no pod name, get workload status
        if not calls:
            calls.append({
                "name": "get_workload_status",
                "arguments": {"namespace": namespace},
            })

        return calls, False

    def _format_final_diagnosis(self, raw_response: str, evidence: list, query: str) -> tuple[str, dict]:
        clean_text = re.sub(r"<think>.*?</think>", "", raw_response, flags=re.DOTALL).strip()
        clean_text = re.sub(r"<tool_call>.*?</tool_call>", "", clean_text, flags=re.DOTALL).strip()

        findings = []
        recommendations = []
        crash_info = []
        image_info = []
        oom_info = []
        pending_info = []
        config_err_info = []

        for item in evidence:
            tool = item.get("tool")
            res = item.get("result", {})
            err = item.get("error")

            if err:
                findings.append(f"Tool {tool} returned error: {err}")
                continue

            if tool == "get_pod_status":
                phase = res.get("phase", "Unknown")
                pod = res.get("pod_name", item.get("arguments", {}).get("pod", "pod"))
                node = res.get("node_name", "") or ""
                if phase == "Pending":
                    findings.append(
                        f"Pod '{pod}' phase: Pending (node: {node or '<not yet scheduled>'})."
                    )
                    unsched = node in ("", "<not yet scheduled>", "<none>")
                    pending_info.append((pod, node, unsched))

                containers = res.get("containers", [])
                for c in containers:
                    is_init = False
                    _classify_source = "container"
                    c_name = c.get("name", "container")
                    comp = f"Container '{c_name}'"
                    state = c.get("state", "")
                    restarts = c.get("restart_count", 0)
                    has_last_term = c.get("has_last_termination", False)
                    last_reason = c.get("last_termination_reason", "")
                    last_exit = c.get("last_termination_exit_code", None)

                    # Completed is a NORMAL termination, not a crash.
                    if last_reason == "Completed" or state.startswith("Terminated (reason: Completed"):
                        findings.append(f"{comp} in pod '{pod}' completed (exit code {last_exit}).")
                        continue

                    if last_reason == "OOMKilled" or "OOMKilled" in state:
                        findings.append(f"{comp} in pod '{pod}' killed due to OOM (Out Of Memory)")
                        oom_info.append((c_name, pod, is_init))
                        recommendations.append("Increase container memory limit or optimize workload memory consumption.")
                    elif "CreateContainerConfigError" in state or "Container cannot run" in state:
                        cfg_msg = state
                        findings.append(f"{comp} in pod '{pod}' state: {cfg_msg}")
                        config_err_info.append((c_name, pod, cfg_msg, is_init))
                    elif last_reason == "Error" and last_exit == 0:
                        findings.append(f"{comp} in pod '{pod}' exited with code 0.")
                    elif "CrashLoopBackOff" in state or "Terminated" in state or has_last_term:
                        exit_str = f"exit code {last_exit}" if last_exit is not None else "failed"
                        findings.append(f"{comp} in pod '{pod}' in CrashLoopBackOff/Terminated state ({exit_str}, restart count: {restarts}).")
                        crash_info.append((c_name, pod, restarts, last_reason, last_exit, is_init))
                    elif "ErrImagePull" in state or "ImagePullBackOff" in state or "CreateImageError" in state or "Image" in state:
                        findings.append(f"{comp} in pod '{pod}' state: {state}")
                        image_info.append((c_name, pod, state, is_init))
                        recommendations.append("Check container image name/tag spelling or configure imagePullSecrets.")
                    elif "OOMKilled" in state:
                        findings.append(f"{comp} in pod '{pod}' killed due to OOM (Out Of Memory)")
                        oom_info.append((c_name, pod, is_init))
                        recommendations.append("Increase container memory limit or optimize workload memory consumption.")
                    elif state:
                        findings.append(f"{comp} status: {state} (restarts: {restarts})")

                # Init containers get the same classification with an init flag.
                for c in (res.get("init_containers", []) or []):
                    c_name = c.get("name", "init")
                    is_init = True
                    comp = f"Init container '{c_name}'"
                    state = c.get("state", "")
                    restarts = c.get("restart_count", 0)
                    has_last_term = c.get("has_last_termination", False)
                    last_reason = c.get("last_termination_reason", "")
                    last_exit = c.get("last_termination_exit_code", None)

                    if last_reason == "Completed" or state.startswith("Terminated (reason: Completed"):
                        findings.append(f"{comp} in pod '{pod}' completed (exit code {last_exit}).")
                    elif last_reason == "OOMKilled" or "OOMKilled" in state:
                        findings.append(f"{comp} in pod '{pod}' killed due to OOM (Out Of Memory)")
                        oom_info.append((c_name, pod, is_init))
                        recommendations.append("Increase container memory limit or optimize workload memory consumption.")
                    elif "CreateContainerConfigError" in state or "Container cannot run" in state:
                        findings.append(f"{comp} in pod '{pod}' state: {state}")
                        config_err_info.append((c_name, pod, state, is_init))
                    elif "CrashLoopBackOff" in state or "Terminated" in state or has_last_term:
                        exit_str = f"exit code {last_exit}" if last_exit is not None else "failed"
                        findings.append(f"{comp} in pod '{pod}' in CrashLoopBackOff/Terminated state ({exit_str}, restart count: {restarts}).")
                        crash_info.append((c_name, pod, restarts, last_reason, last_exit, is_init))
                    elif "ErrImagePull" in state or "ImagePullBackOff" in state or "CreateImageError" in state or "Image" in state:
                        findings.append(f"{comp} in pod '{pod}' state: {state}")
                        image_info.append((c_name, pod, state, is_init))
                    elif state:
                        findings.append(f"{comp} status: {state} (restarts: {restarts})")

            elif tool == "get_workload_status":
                if isinstance(res, dict) and res:
                    owners = res.get("owner_references", []) or []
                    if owners:
                        o = owners[0]
                        findings.append(
                            f"Workload owner of pod '{res.get('pod_name', '?')}': {o.get('kind')} '{o.get('name')}'."
                        )
                elif isinstance(res, dict) and res.get("name"):
                    desired = res.get("desired_replicas")
                    ready = res.get("ready_replicas")
                    findings.append(
                        f"Workload '{res.get('name')}': desired replicas {desired}, ready replicas {ready}."
                    )

            elif tool == "get_node_status":
                if isinstance(res, dict) and res:
                    n_name = res.get("name", "node")
                    n_status = res.get("status", "Unknown")
                    resources = res.get("resources", {}) or {}
                    sysinfo = res.get("system_info", {}) or {}
                    findings.append(
                        f"Node '{n_name}' status: {n_status}. "
                        f"CPU capacity {resources.get('cpu_capacity', '?')}, allocatable {resources.get('cpu_allocatable', '?')}; "
                        f"memory capacity {resources.get('memory_capacity', '?')}, allocatable {resources.get('memory_allocatable', '?')}."
                    )
                    if sysinfo:
                        findings.append(
                            f"Node '{n_name}' system: kubelet {sysinfo.get('kubelet_version', '?')}, "
                            f"OS {sysinfo.get('operating_system', '?')}, arch {sysinfo.get('architecture', '?')}, "
                            f"kernel {sysinfo.get('kernel_version', '?')}, runtime {sysinfo.get('container_runtime', '?')}."
                        )

            elif tool == "get_pod_events":
                events = res if isinstance(res, list) else []
                for ev in events:
                    reason = ev.get("reason", "")
                    msg = ev.get("message", "")
                    if reason in ["Failed", "BackOff", "FailedScheduling", "Unhealthy", "OOMKilled", "ErrImagePull"]:
                        findings.append(f"Kubernetes Event [{reason}]: {msg}")

            elif tool == "get_container_logs":
                c_name = item.get("arguments", {}).get("container", "container")
                if isinstance(res, dict):
                    lines = res.get("lines", [])
                    if lines:
                        log_str = " | ".join(lines)
                        findings.append(f"Container '{c_name}' logs: {log_str}")
                    elif res.get("unavailable"):
                        findings.append(f"Container '{c_name}' logs unavailable: {res.get('unavailable_reason')}")
                    elif res.get("api_error"):
                        findings.append(f"Container '{c_name}' log error: {res.get('api_error_msg') or res.get('api_error_message')}")

        diag_title = "Workload failure detected in cluster."
        root_cause = ""
        if not findings:
            diag_title = clean_text if clean_text else "Unable to establish root cause from evidence."
            findings.append("Query processed with evidence collection.")
        # Question-domain evidence from the domain tools takes precedence over
        # any pod health signal (e.g. CrashLoopBackOff must not hijack a GPU,
        # storage, service, configuration, or workload question).
        not_found = next((it for it in evidence if _is_resource_not_found(it)), None)
        if not_found:
            tool = not_found.get("tool")
            args = not_found.get("arguments", {}) or {}
            ns = args.get("namespace", "?")
            target = (
                args.get("pod") or args.get("node_name") or args.get("workload_name")
                or args.get("service_name") or args.get("pvc_name") or args.get("namespace", "?")
            )
            diag_title = (
                f"The requested resource '{target}' in namespace '{ns}' could not be found "
                f"in the cluster."
            )
            try:
                err_detail = str(not_found.get("error") or not_found.get("result") or "")
            except Exception:
                err_detail = ""
            root_cause = (
                f"Provider reported RESOURCE_NOT_FOUND for {tool}(namespace={ns!r}, target={target!r}): {err_detail}. "
                f"The resource does not exist (or is not visible with current permissions). No workload failure, "
                f"application crash, or container error is proven by this absence of evidence."
            )
            recommendations = [
                "Verify the resource name and namespace spelling, and check RBAC permissions."
            ]
            diag_dict = {
                "diagnosis": diag_title,
                "evidence": findings,
                "root_cause": root_cause,
                "recommended_next_step": recommendations[0],
            }
            out = f"Diagnosis:\n{diag_title}\n\nEvidence:\n"
            for f in findings:
                out += f"- {f}\n"
            out += f"\nRoot Cause:\n{root_cause}\n"
            out += f"\nRecommended next step:\n- {recommendations[0]}\n"
            return out, diag_dict

        def _domain_result(tool):
            """Return the tool's result when executed (even if empty), else None."""
            for it in evidence:
                if it.get("tool") == tool:
                    if it.get("error"):
                        return {"_domain_error": it["error"]}
                    return it.get("result")
            return None

        gpu_res = _domain_result("get_gpu_status")
        storage_res = _domain_result("get_storage_status")
        service_res = _domain_result("get_service_status")
        config_res = _domain_result("get_configuration_status")
        workload_res = _domain_result("get_workload_status")

        domain_executed = {
            "gpu": gpu_res is not None,
            "storage": storage_res is not None,
            "service": service_res is not None,
            "config": config_res is not None,
            "workload": workload_res is not None,
        }
        domain_evidence_present = any(domain_executed.values()) or any(
            f.startswith("Node '") for f in findings
        )
        if domain_evidence_present:
            # A domain-specific answer must not be hijacked by pod health noise
            # that only appeared because get_pod_status ran for discovery.
            findings = [f for f in findings if not (
                f.startswith("Container '") and ("CrashLoopBackOff" in f or "Terminated state" in f)
            )]
            crash_info = []

        def _ok(res):
            return res is not None and not (isinstance(res, dict) and "_domain_error" in res)

        if _ok(gpu_res):
            node = gpu_res.get("node_name", "node")
            exposed = gpu_res.get("gpu_resource_exposed")
            available = gpu_res.get("gpu_available")
            findings.append(
                f"GPU status of node '{node}': gpu_available={available}, gpu_resource_exposed={exposed}"
                + (f", resource={gpu_res.get('resource_name')}" if gpu_res.get('resource_name') else "")
                + (f", model={gpu_res.get('model')}" if gpu_res.get('model') else "")
            )
            if exposed:
                diag_title = (
                    f"Node '{node}' exposes GPU resources to the cluster."
                )
                root_cause = (
                    f"The node advertises GPU extended resources (capacity/allocatable reported by the "
                    f"device plugin), so GPU workloads can be scheduled onto it. Current availability: {available}."
                )
            else:
                diag_title = (
                    f"Node '{node}' does NOT expose any GPU resources to the cluster."
                )
                root_cause = (
                    f"Kubernetes reports gpu_resource_exposed=false on this node: no nvidia.com/gpu or similar "
                    f"extended resource is advertised, so GPU workloads cannot be scheduled here. This is the "
                    f"node's configuration state, independent of whether a pod is in CrashLoopBackOff."
                )
            recommendations = ["Check node labels/device plugin or NFD labels if GPUs were expected on this node."]

        elif _ok(storage_res):
            if "claims" in storage_res:
                claims = storage_res.get("claims") or []
                if claims:
                    for cl in claims:
                        findings.append(
                            f"PVC '{cl.get('name')}' in '{cl.get('namespace')}': phase {cl.get('phase')}, "
                            f"storage class {cl.get('storage_class_name')}, requested {cl.get('requested_storage')}, "
                            f"access modes {cl.get('access_modes')}."
                        )
                    diag_title = f"The pod uses {len(claims)} persistent volume claim(s)."
                    root_cause = "Storage for this pod is backed by the PVCs listed above; their phase and storage class are the evidence of storage health."
                else:
                    findings.append(f"Pod '{storage_res.get('pod_name')}' has no persistentVolumeClaim volumes.")
                    diag_title = "This pod does not use any persistent storage (no PVCs)."
                    root_cause = "The pod's volumes contain no persistentVolumeClaim entries, so there is no PVC-backed storage status to report."
            elif "storage_classes" in storage_res:
                scs = storage_res.get("storage_classes") or []
                names = [sc.get("name") for sc in scs]
                findings.append(f"Cluster storage classes: {', '.join(n for n in names if n) or 'none'}.")
                diag_title = f"Storage classes available in the cluster: {len(scs)}."
                root_cause = "No specific PVC was requested; the storage status reflects the cluster's storage classes."
            recommendations = ["Verify each PVC phase is Bound and its storage class provisioner is healthy."]

        elif _ok(service_res):
            services = service_res if isinstance(service_res, list) else [service_res]
            services = [s for s in services if isinstance(s, dict)]
            if services:
                for s in services:
                    subsets = s.get("subsets") or []
                    ready = sum(x.get("ready_address_count", 0) for x in subsets)
                    notready = sum(x.get("not_ready_address_count", 0) for x in subsets)
                    findings.append(
                        f"Service '{s.get('name')}' ({s.get('type')}), cluster IP {s.get('cluster_ip')}, "
                        f"ready endpoints {ready}, not-ready endpoints {notready}."
                    )
                diag_title = f"Found {len(services)} service(s) in the namespace with endpoint readiness as above."
                root_cause = "Service/network status is determined by the Service objects and their endpoint subsets; ready vs not-ready endpoint counts show whether traffic can be routed."
            else:
                findings.append("No services found in the namespace.")
                diag_title = "No services are defined in this namespace."
                root_cause = "The provider returned no Service objects, so there is no service/network status to report for this pod's namespace."
            recommendations = ["Check Service selectors vs pod labels if endpoints show 0 ready addresses."]

        elif _ok(config_res) and isinstance(config_res, dict):
            sas = config_res.get("service_account")
            cms = config_res.get("config_maps") or []
            secs = config_res.get("secrets") or []
            ips = config_res.get("image_pull_secrets") or []
            findings.append(
                f"Configuration/identity for pod '{config_res.get('pod_name')}': "
                f"service account '{(sas or {}).get('name')}', {len(cms)} config map(s), "
                f"{len(secs)} secret reference(s), image pull secrets {ips}."
            )
            for cm in cms:
                findings.append(f"ConfigMap '{cm.get('name')}' keys: {cm.get('keys')}.")
            for sec in secs:
                findings.append(f"Secret '{sec.get('name')}' (type {sec.get('type')}) keys: {sec.get('keys')}; values never collected.")
            diag_title = "Configuration and identity settings were retrieved for this pod."
            root_cause = "The pod's env/volume references resolve to the config maps, secrets (metadata only, values never collected), service account, and image pull secrets listed above."
            recommendations = ["Verify referenced config maps/secrets exist in the same namespace and that the service account has the required RBAC."]

        elif _ok(workload_res) and isinstance(workload_res, dict) and workload_res.get("owner_references"):
            owners = workload_res["owner_references"]
            o = owners[0]
            findings.append(f"Pod is owned by {o.get('kind')} '{o.get('name')}' (controller={o.get('controller')}).")
            if crash_info:
                diag_title = (
                    f"The workload owning this pod is {o.get('kind')} '{o.get('name')}'. Its managed pod is "
                    f"not becoming healthy: it is in CrashLoopBackOff because the container process exits with code 1."
                )
                root_cause = "The workload controller keeps recreating a pod whose container exits with code 1; fixing the container process is required for the workload to become healthy."
            else:
                diag_title = f"The workload owning this pod is {o.get('kind')} '{o.get('name')}'."
                root_cause = "The pod's owner reference identifies the controlling workload."
            recommendations = ["Inspect the owning controller's desired vs ready replica state."]

        if not any(_ok(r) for r in (gpu_res, storage_res, service_res, config_res, workload_res)) and any(domain_executed.values()):
            diag_title = "The requested domain evidence could not be retrieved from the cluster."
            root_cause = (
                "The relevant tool was invoked but returned an error or no usable result. "
                "Nothing is inferred beyond the collected evidence."
            )
            recommendations = ["Retry the diagnostic or inspect the tool error above."]

        completed_all = False
        for it in evidence:
            if it.get("tool") == "get_pod_status" and not it.get("error"):
                r = it.get("result")
                if isinstance(r, dict) and r.get("phase") == "Succeeded":
                    cs = (r.get("containers") or []) + (r.get("init_containers") or [])
                    if cs and all(
                        c.get("last_termination_reason") == "Completed" for c in cs if isinstance(c, dict)
                    ):
                        completed_all = True

        probe_info = []
        for it in evidence:
            if it.get("tool") == "get_pod_events" and isinstance(it.get("result"), list):
                for ev in it["result"]:
                    if not isinstance(ev, dict):
                        continue
                    msg = str(ev.get("message", ""))
                    if "Liveness probe failed" in msg:
                        probe_info.append(("liveness", msg))
                    elif "Startup probe failed" in msg:
                        probe_info.append(("startup", msg))
                    elif "Readiness probe failed" in msg:
                        probe_info.append(("readiness", msg))

        node_findings = [f for f in findings if f.startswith("Node '")]
        if node_findings:
            healthy = any("status: Ready" in f for f in node_findings)
            if healthy:
                diag_title = (
                    "The node running the pod is healthy: it reports status Ready along with its "
                    "CPU/memory capacity and allocatable resources."
                )
                root_cause = (
                    "The target node is Ready and reports capacity/allocatable CPU and memory. "
                    "The collected evidence shows no node-level health problem. Container- or app-level "
                    "failures (e.g. CrashLoopBackOff of a pod) are separate from node health and were "
                    "not collected because the question targeted the node."
                )
                recommendations = [
                    "Review the node's allocatable CPU/memory against pod requests/limits to plan headroom."
                ]
            else:
                diag_title = "The node running the pod is NOT Ready according to Kubernetes node status."
                root_cause = (
                    "Kubernetes reports the node status as not-Ready. Check kubelet, node conditions, "
                    "and cluster events for the specific cause."
                )
                recommendations = ["Inspect node conditions and kubelet logs on the affected node."]
        if pending_info:
            # Scheduling diagnosis is valid ONLY when the pod really cannot be
            # scheduled: phase Pending AND no node assigned AND no Scheduled
            # event. A pod with a node assigned is scheduled — its Pending
            # state must be explained by container states/events instead.
            pod, node, _unsched_by_node = pending_info[0]
            all_events = []
            for it in evidence:
                if it.get("tool") == "get_pod_events" and isinstance(it.get("result"), list):
                    all_events.extend(e for e in it["result"] if isinstance(e, dict))

            scheduled_event = any(
                e.get("reason") == "Scheduled" or "Successfully assigned" in str(e.get("message", ""))
                for e in all_events
            )
            node_assigned = node not in ("", "<not yet scheduled>", "<none>")
            truly_unscheduled = not node_assigned and not scheduled_event

            sched_msgs = []
            for ev in all_events:
                if ev.get("reason") in ("FailedScheduling", "FailedCreate") or re.search(
                    r"Insufficient|node affinity|didn't match.*selector|taint|no nodes available|preemption",
                    str(ev.get("message", "")),
                    re.IGNORECASE,
                ):
                    sched_msgs.append((ev.get("reason", ""), str(ev.get("message", ""))))

            if sched_msgs and truly_unscheduled:
                reason, msg = sched_msgs[0]
                diag_title = "The pod is stuck in Pending because it cannot be scheduled."
                root_cause = (
                    f"Kubernetes scheduler reported {reason or 'FailedScheduling'}: {msg} "
                    f"The pod has no node assigned, so container logs cannot exist yet. "
                    f"The scheduling event is the primary root cause evidence."
                )
                recommendations = [
                    "Free node capacity or relax the pod's scheduling constraints (CPU/memory requests, nodeSelector/affinity, tolerations)."
                ]
            elif not truly_unscheduled:
                # Scheduled but still Pending: explain via container state / events.
                wait_reason, wait_msg = self._first_container_wait(evidence, pod)
                if wait_reason:
                    if "CreateContainerConfigError" in wait_reason or re.search(r"configmap|secret", wait_msg, re.I):
                        diag_title = (
                            f"The pod is scheduled on node '{node}' but its container cannot be created "
                            f"due to a configuration dependency error."
                        )
                        root_cause = (
                            f"Container state: {wait_reason}: {wait_msg}. "
                            f"A referenced ConfigMap/Secret is missing or invalid, so the container runtime "
                            f"cannot start the container. This is NOT a scheduling problem — the pod is already "
                            f"assigned to node '{node}'."
                        )
                        recommendations = [
                            "Create the missing ConfigMap/Secret in the pod's namespace or fix the reference name."
                        ]
                    elif re.search(r"ErrImagePull|ImagePullBackOff|CreateImage", wait_reason + wait_msg, re.I):
                        diag_title = (
                            f"The pod is scheduled on node '{node}' but its container cannot start because "
                            f"the image cannot be pulled."
                        )
                        root_cause = (
                            f"Container state: {wait_reason}: {wait_msg}. The image pull failed, so the "
                            f"container never starts."
                        )
                        recommendations = ["Check image name/tag and registry credentials."]
                    else:
                        diag_title = (
                            f"The pod is scheduled on node '{node}' but its container is waiting: {wait_reason}."
                        )
                        root_cause = f"Container state: {wait_reason}: {wait_msg}"
                        recommendations = ["Inspect the container state and recent events for this pod."]
                else:
                    cfg_ev = next(
                        (e for e in all_events
                         if re.search(r"configmap|secret", str(e.get("message", "")), re.I)
                         or e.get("reason") == "Failed"),
                        None,
                    )
                    if cfg_ev:
                        diag_title = "The pod is scheduled but failed to start a container."
                        root_cause = f"Event [{cfg_ev.get('reason')}]: {cfg_ev.get('message')}"
                        if re.search(r"configmap|secret", str(cfg_ev.get("message", "")), re.I):
                            recommendations = ["Create or fix the referenced ConfigMap/Secret."]
                            root_cause += " A missing configuration dependency prevents container creation."
                        else:
                            recommendations = ["Inspect the pod events and container statuses."]
                    else:
                        if config_err_info:
                            cn, cp, cs, cinit = config_err_info[0]
                            comp = f"Init container '{cn}'" if cinit else f"Container '{cn}'"
                            diag_title = f"{comp} cannot be created due to a configuration dependency error."
                            root_cause = f"Container state: {cs}. A missing or invalid ConfigMap/Secret reference prevents container creation."
                            recommendations = ["Create the missing ConfigMap/Secret in the pod's namespace or fix the reference."]
                        elif crash_info:
                            cn, cp, cr, crsn, crexit, cinit = crash_info[0]
                            comp = f"Init container '{cn}'" if cinit else f"Container '{cn}'"
                            diag_title = (
                                f"{comp} keeps failing (reason: {crsn or 'Error'}, exit code {crexit}), so the pod never becomes Ready."
                            )
                            root_cause = (
                                f"{comp} in pod '{cp}' terminated repeatedly ({cr} restarts, reason '{crsn}', "
                                f"exit code {crexit}). The main application container may never have started."
                            )
                            recommendations = ["Inspect the failing container's logs and exit reason."]
                        else:
                            diag_title = "The pod is scheduled but still Pending."
                            root_cause = (
                                "The pod is assigned to a node but no container is running and no scheduler "
                                "error was found. Inspect container statuses and recent events."
                            )
                            recommendations = ["Inspect pod events and container states."]
            else:
                diag_title = "The pod is stuck in Pending."
                root_cause = (
                    "The pod phase is Pending and no useful scheduling event was collected. "
                    "Check scheduler events and node capacity. No container has started, so logs are not expected."
                )
                recommendations = ["Inspect get_pod_events output and node allocatable capacity."]
        elif completed_all:
            diag_title = "All containers completed successfully (exit code 0)."
            root_cause = (
                "Every container terminated with reason 'Completed' and exit code 0, which is a normal "
                "successful termination — NOT a failure or a crash."
            )
            recommendations = ["No action needed; treat this as expected Job/task completion."]
        elif crash_info:
            crash = crash_info[0] if crash_info else None
            image_fail = image_info[0] if image_info else None
            oom = oom_info[0] if oom_info else None

            if probe_info and probe_info[0][0] in ("liveness", "startup"):
                kind, msg = probe_info[0]
                if kind == "liveness":
                    diag_title = "The container is being restarted because its liveness probe is failing."
                    root_cause = f"Kubernetes reports: {msg}. A failing liveness probe causes kubelet to restart the container."
                    recommendations = ["Fix the endpoint/path/port used by the liveness probe, or adjust its initial delay/period."]
                else:
                    diag_title = "The container has not completed startup because its startup probe is failing."
                    root_cause = f"Kubernetes reports: {msg}. A failing startup probe prevents the container from being considered started."
                    recommendations = ["Fix the application startup path/port or adjust the startup probe thresholds."]
            elif crash:
                c_name, pod, restarts, last_reason, last_exit, is_init = crash
                comp = f"Init container '{c_name}'" if is_init else f"Container '{c_name}'"
                exit_part = f"exit code {last_exit}" if last_exit is not None else "a non-zero exit"
                diag_title = (
                    f"The pod is repeatedly failing because the application process inside container "
                    f"'{c_name}' exits with {exit_part}"
                    + (f" (last termination reason: {last_reason})" if last_reason else "")
                    + ". Kubernetes repeatedly restarts the failed container, resulting in CrashLoopBackOff."
                )
                root_cause = (
                    f"The application process in container '{c_name}' terminates unsuccessfully "
                    f"({exit_part}, {restarts} restarts). This causes Kubernetes to restart it repeatedly "
                    f"and report BackOff events. The collected evidence proves an application/container "
                    f"process failure but does NOT, by itself, identify a specific internal exception "
                    f"or code-level bug."
                )
                recommendations = [
                    f"Inspect and fix the application command/process in container '{c_name}' that causes "
                    f"{exit_part}. Do not assume a specific internal exception unless logs prove one."
                ]
            elif image_fail:
                c_name, pod, state, is_init = image_fail
                diag_title = (
                    f"Container '{c_name}' in pod '{pod}' cannot start because its image could not be "
                    f"pulled ({state}). Kubernetes keeps retrying, producing ImagePullBackOff."
                )
                root_cause = (
                    f"The container image for '{c_name}' cannot be pulled, so the container never starts. "
                    f"This is proven by the container state '{state}' and pull-related events."
                )
                recommendations = [
                    "Check the image name/tag spelling, registry availability, and imagePullSecrets."
                ]
            elif oom:
                c_name, pod, is_init = oom
                diag_title = (
                    f"Container '{c_name}' in pod '{pod}' was killed because it exceeded its memory limit (OOMKilled)."
                )
                root_cause = (
                    f"The container process was terminated by the kernel OOM killer after exceeding its "
                    f"memory limit. Kubernetes reports the termination reason OOMKilled."
                )
                recommendations = [
                    "Increase the container memory limit or reduce its memory consumption."
                ]
            elif any("BackOff" in f for f in findings):
                diag_title = (
                    "Kubernetes is backing off while restarting a failing container; the container is "
                    "repeatedly failing to stay up."
                )
                root_cause = (
                    "BackOff events prove Kubernetes is repeatedly restarting a failing container, but the "
                    "collected evidence does not yet identify the exact internal failure. Container logs are "
                    "needed to determine why the process exits."
                )
        elif image_info:
            c_name, pod, state, is_init = image_info[0]
            comp = f"Init container '{c_name}'" if is_init else f"Container '{c_name}'"
            diag_title = (
                f"{comp} in pod '{pod}' cannot start because its image could not be "
                f"pulled ({state}). Kubernetes keeps retrying, producing ImagePullBackOff."
            )
            root_cause = (
                f"The container image for '{c_name}' cannot be pulled, so the container never starts. "
                f"This is proven by the container state '{state}' and pull-related events."
            )
            recommendations = ["Check the image name/tag spelling, registry availability, and imagePullSecrets."]
        elif oom_info:
            c_name, pod, is_init = oom_info[0]
            comp = f"Init container '{c_name}'" if is_init else f"Container '{c_name}'"
            diag_title = (
                f"{comp} in pod '{pod}' was killed because it exceeded its memory limit (OOMKilled)."
            )
            root_cause = (
                "The container process was terminated by the kernel OOM killer after exceeding its "
                "memory limit. Kubernetes reports the termination reason OOMKilled."
            )
            recommendations = ["Increase the container memory limit or reduce its memory consumption."]
        elif config_err_info:
            c_name, pod, state, is_init = config_err_info[0]
            comp = f"Init container '{c_name}'" if is_init else f"Container '{c_name}'"
            diag_title = (
                f"{comp} in pod '{pod}' cannot start due to a configuration dependency error."
            )
            root_cause = (
                f"Container state: {state}. A referenced ConfigMap/Secret is missing or invalid, "
                f"so the container runtime cannot create the container."
            )
            recommendations = ["Create the missing ConfigMap/Secret in the pod's namespace or fix the reference."]
        elif probe_info:
            kind, msg = probe_info[0]
            if kind == "liveness":
                diag_title = "The container is being restarted because its liveness probe is failing."
                root_cause = f"Kubernetes reports: {msg}. A failing liveness probe causes kubelet to restart the container."
                recommendations = ["Fix the endpoint/path/port used by the liveness probe, or adjust its initial delay/period."]
            elif kind == "startup":
                diag_title = "The container has not completed startup because its startup probe is failing."
                root_cause = f"Kubernetes reports: {msg}. A failing startup probe prevents the container from being considered started."
                recommendations = ["Fix the application startup path/port or adjust the startup probe thresholds."]
            else:
                diag_title = "The container is running but failing readiness checks, so it is removed from service endpoints."
                root_cause = f"Kubernetes reports: {msg}. Readiness failure does NOT restart the container; it only affects Service endpoint membership."
                recommendations = ["Fix the readiness endpoint/path/port or correct the dependency the check depends on."]
        elif any("BackOff" in f for f in findings):
            diag_title = (
                "Kubernetes is backing off while restarting a failing container; the container is "
                "repeatedly failing to stay up."
            )
            root_cause = (
                "BackOff events prove Kubernetes is repeatedly restarting a failing container, but the "
                "collected evidence does not yet identify the exact internal failure. Container logs are "
                "needed to determine why the process exits."
            )

        rec_str = recommendations[0] if recommendations else "Inspect pod events and logs for detailed container failure reasons."

        diag_dict = {
            "diagnosis": diag_title,
            "evidence": findings,
            "root_cause": root_cause,
            "recommended_next_step": rec_str,
        }

        diagnosis_str = f"Diagnosis:\n{diag_title}\n\nEvidence:\n"
        for f in findings:
            diagnosis_str += f"- {f}\n"

        if root_cause:
            diagnosis_str += f"\nRoot Cause:\n{root_cause}\n"

        diagnosis_str += f"\nRecommended next step:\n- {rec_str}\n"

        return diagnosis_str, diag_dict
