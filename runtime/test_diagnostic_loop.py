#!/usr/bin/env python3
"""
Feature 22 — Unit Tests for Real-Time Diagnostic Loop
Coverage:
1. Valid get_pod_status call parsing & validation
2. Valid get_pod_events call parsing & validation
3. Invalid tool name handling
4. Missing required argument validation
5. Malformed tool call JSON handling
6. Provider error handling
7. Maximum loop/iteration protection
8. Full ImagePullBackOff diagnosis formatting flow
"""

import unittest
from runtime.diagnostic_loop import (
    parse_tool_calls,
    validate_tool_call,
    ToolDispatcherBridge,
    NeedleDiagnoser,
)


class MockDiagnoser:
    """Mock diagnoser for testing iteration limits and conversation flow without loading JAX model."""

    def __init__(self, responses):
        self.responses = responses
        self.call_count = 0
        self.tools_schema = []

    def generate_step(self, prompt: str, max_tokens: int = 300, seed: int = 42) -> str:
        if self.call_count < len(self.responses):
            resp = self.responses[self.call_count]
            self.call_count += 1
            return resp
        return "Diagnosis:\nWorkload operating normally.\nEvidence:\n- None\nRecommended next step:\n- None"

    def diagnose(self, query: str, bridge: ToolDispatcherBridge, max_iterations: int = 5, verbose: bool = False):
        from runtime.diagnostic_loop import parse_tool_calls, validate_tool_call
        import json

        conversation_prompt = query
        evidence_collected = []
        executed_calls = set()
        iterations = 0
        final_raw_response = ""

        while iterations < max_iterations:
            iterations += 1
            raw_response = self.generate_step(conversation_prompt)
            final_raw_response = raw_response

            tool_calls = parse_tool_calls(raw_response)
            if not tool_calls:
                break

            valid_calls_executed = 0
            for call in tool_calls:
                is_valid, err_msg = validate_tool_call(call)
                if not is_valid:
                    continue

                tool_name = call["name"]
                args = call["arguments"]
                call_key = (tool_name, json.dumps(args, sort_keys=True))

                if call_key in executed_calls:
                    continue

                executed_calls.add(call_key)
                valid_calls_executed += 1

                evidence = bridge.execute_tool(tool_name, args)
                evidence_collected.append(evidence)

            if valid_calls_executed == 0:
                break

        final_text, diag_dict = NeedleDiagnoser._format_final_diagnosis(self, final_raw_response, evidence_collected, query)

        return {
            "query": query,
            "iterations": iterations,
            "evidence_collected": evidence_collected,
            "raw_response": final_raw_response,
            "final_diagnosis": final_text,
        }


class TestDiagnosticLoop(unittest.TestCase):

    def test_01_parse_valid_tool_call(self):
        raw = '<think>Checking pod status</think>\n<tool_call>[{"name": "get_pod_status", "arguments": {"namespace": "default", "pod": "test-pod"}}]</tool_call>'
        calls = parse_tool_calls(raw)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["name"], "get_pod_status")
        self.assertEqual(calls[0]["arguments"]["pod"], "test-pod")

    def test_02_parse_valid_get_pod_events(self):
        raw = '<tool_call>{"name": "get_pod_events", "arguments": {"namespace": "prod", "pod": "app-01"}}</tool_call>'
        calls = parse_tool_calls(raw)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["name"], "get_pod_events")
        self.assertEqual(calls[0]["arguments"]["namespace"], "prod")

    def test_03_invalid_tool_name(self):
        call = {"name": "invalid_k8s_tool", "arguments": {"namespace": "default"}}
        is_valid, msg = validate_tool_call(call)
        self.assertFalse(is_valid)
        self.assertIn("Unknown tool name", msg)

    def test_04_missing_required_argument(self):
        call = {"name": "get_pod_status", "arguments": {"namespace": "default"}}
        is_valid, msg = validate_tool_call(call)
        self.assertFalse(is_valid)
        self.assertIn("missing required argument 'pod'", msg)

    def test_05_malformed_tool_call_json(self):
        raw = '<tool_call>{invalid json string}</tool_call>'
        calls = parse_tool_calls(raw)
        self.assertEqual(len(calls), 1)
        self.assertTrue(calls[0].get("_invalid"))

    def test_06_provider_error_handling(self):
        bridge = ToolDispatcherBridge(provider_type="fake")
        evidence = bridge.execute_tool("get_pod_status", {"namespace": "default", "pod": "non-existent-pod"})
        self.assertIn("error", evidence)
        self.assertIn("RESOURCE_NOT_FOUND", evidence["error"])

    def test_07_maximum_loop_iteration_protection(self):
        # Repeat the same valid tool call infinitely
        repeating_response = '<tool_call>{"name": "get_pod_status", "arguments": {"namespace": "default", "pod": "test-pod"}}</tool_call>'
        diagnoser = MockDiagnoser([repeating_response] * 10)
        bridge = ToolDispatcherBridge(provider_type="fake")

        res = diagnoser.diagnose("Why is pod test-pod failing?", bridge=bridge, max_iterations=3)
        self.assertLessEqual(res["iterations"], 3)
        self.assertEqual(len(res["evidence_collected"]), 1)  # Duplicate skipped

    def test_08_full_imagepullbackoff_flow(self):
        responses = [
            '<tool_call>{"name": "get_pod_status", "arguments": {"namespace": "default", "pod": "f18-imagepull-001"}}</tool_call>',
            '<tool_call>{"name": "get_pod_events", "arguments": {"namespace": "default", "pod": "f18-imagepull-001"}}</tool_call>',
            'Diagnosis:\nContainer image could not be pulled.\nEvidence:\n- ErrImagePull in default/f18-imagepull-001\nRecommended next step:\nFix image name.',
        ]
        diagnoser = MockDiagnoser(responses)
        bridge = ToolDispatcherBridge(provider_type="fake")

        res = diagnoser.diagnose("Why is pod f18-imagepull-001 failing?", bridge=bridge)
        self.assertEqual(res["iterations"], 3)
        self.assertEqual(len(res["evidence_collected"]), 2)
        self.assertIn("Diagnosis:", res["final_diagnosis"])

    def test_09_container_name_propagation_from_pod_status(self):
        pod_name = "crashloop-test-896754f5f-n9rmf"
        namespace = "ai-diagnose-test"

        class MockBridge:
            def __init__(self):
                self.provider_type = "mock"
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                if tool_name == "get_pod_status":
                    return {
                        "tool": tool_name,
                        "arguments": arguments,
                        "result": {
                            "pod_name": pod_name,
                            "namespace": namespace,
                            "phase": "Running",
                            "containers": [
                                {
                                    "name": "busybox",
                                    "image": "busybox:1.36",
                                    "restart_count": 5,
                                    "state": "Terminated (reason: Error, exit: 1)",
                                }
                            ],
                        },
                    }
                elif tool_name == "get_container_logs":
                    return {
                        "tool": tool_name,
                        "arguments": arguments,
                        "result": {
                            "lines": ["Application starting...", "Application crashed"],
                        },
                    }
                return {"tool": tool_name, "arguments": arguments, "result": {}}

        mock_bridge = MockBridge()

        diagnoser = object.__new__(NeedleDiagnoser)
        diagnoser._fixed_buf_len = 1280
        diagnoser.tools_schema = []
        diagnoser.generate_step = lambda prompt, max_tokens=60, seed=42: "Diagnosis:\nCrashLoopBackOff\nEvidence:\n- Application crashed\nRecommended next step:\nFix application error"

        res = diagnoser.diagnose(f"Why is pod {pod_name} failing in namespace {namespace}?", bridge=mock_bridge)

        log_calls = [c for c in mock_bridge.calls if c["tool"] == "get_container_logs"]
        self.assertTrue(len(log_calls) > 0, "get_container_logs was not called")
        self.assertEqual(log_calls[0]["arguments"]["container"], "busybox")
        self.assertNotEqual(log_calls[0]["arguments"]["container"], pod_name)


    def _make_bare_diagnoser(self, captured: dict = None):
        diagnoser = object.__new__(NeedleDiagnoser)
        diagnoser._fixed_buf_len = 1280
        diagnoser.tools_schema = []

        def fake_generate(prompt, max_tokens=60, seed=42):
            if captured is not None:
                captured["prompt"] = prompt
            return "Diagnosis:\nCrashLoopBackOff\nEvidence:\n- crashed\nRecommended next step:\nFix app"

        diagnoser.generate_step = fake_generate
        return diagnoser

    def test_10_explicit_target_used_for_first_tool_call(self):
        """A. Explicit namespace/pod must drive the first get_pod_status call."""
        namespace = "ai-diagnose-test"
        pod_name = "crashloop-test-896754f5f-n9rmf"

        class MockBridge:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                return {"tool": tool_name, "arguments": arguments, "result": {"containers": []}}

        mock_bridge = MockBridge()
        diagnoser = self._make_bare_diagnoser()
        diagnoser.diagnose(
            "Why is this pod repeatedly failing?",
            bridge=mock_bridge,
            namespace=namespace,
            pod=pod_name,
        )

        first = mock_bridge.calls[0]
        self.assertEqual(first["tool"], "get_pod_status")
        self.assertEqual(first["arguments"]["namespace"], namespace)
        self.assertEqual(first["arguments"]["pod"], pod_name)

    def test_11_question_word_never_replaces_supplied_pod(self):
        """B. The runtime must never substitute the pod with a word from the question."""
        namespace = "ai-diagnose-test"
        pod_name = "crashloop-test-896754f5f-n9rmf"

        class MockBridge:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                return {"tool": tool_name, "arguments": arguments, "result": {"containers": []}}

        mock_bridge = MockBridge()
        diagnoser = self._make_bare_diagnoser()
        diagnoser.diagnose(
            "Why is this pod repeatedly failing?",
            bridge=mock_bridge,
            namespace=namespace,
            pod=pod_name,
        )

        for call in mock_bridge.calls:
            for v in call["arguments"].values():
                self.assertNotEqual(v, "repeatedly")

    def test_12_target_visible_in_needle_prompt(self):
        """Explicit namespace/pod must be present in the Needle synthesis prompt."""
        captured = {}

        class MockBridge:
            provider_type = "mock"

            def execute_tool(self, tool_name, arguments):
                return {"tool": tool_name, "arguments": arguments, "result": {}}

        diagnoser = self._make_bare_diagnoser(captured)
        diagnoser.diagnose(
            "Why is pod web-7d9f failing?",
            bridge=MockBridge(),
            namespace="prod-x",
            pod="web-7d9f",
        )

        self.assertIn("Target namespace: prod-x", captured.get("prompt", ""))
        self.assertIn("Target pod: web-7d9f", captured.get("prompt", ""))

    def test_13_container_name_propagation_generic(self):
        """D/E. Arbitrary pod/container names must be propagated (not hardcoded)."""
        namespace = "prod-x"
        pod_name = "web-7d9f-abc12"
        container_name = "app-server"

        class MockBridge:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                if tool_name == "get_pod_status":
                    return {
                        "tool": tool_name,
                        "arguments": arguments,
                        "result": {
                            "pod_name": pod_name,
                            "namespace": namespace,
                            "containers": [{"name": container_name, "image": "app:2.0"}],
                        },
                    }
                return {"tool": tool_name, "arguments": arguments, "result": {"lines": ["ok"]}}

        mock_bridge = MockBridge()
        diagnoser = self._make_bare_diagnoser()
        diagnoser.diagnose(
            f"Why is pod {pod_name} failing?",
            bridge=mock_bridge,
            namespace=namespace,
            pod=pod_name,
        )

        log_calls = [c for c in mock_bridge.calls if c["tool"] == "get_container_logs"]
        self.assertTrue(log_calls, "get_container_logs was not called")
        self.assertEqual(log_calls[0]["arguments"]["container"], container_name)
        self.assertNotEqual(log_calls[0]["arguments"]["container"], pod_name)


    def _fmt(self, evidence):
        diagnoser = object.__new__(NeedleDiagnoser)
        return diagnoser._format_final_diagnosis("", evidence, "q")

    def test_14_crashloop_specific_diagnosis(self):
        """A/B/C/D/E: CrashLoop evidence yields a specific, grounded diagnosis."""
        evidence = [
            {"tool": "get_pod_status", "arguments": {"namespace": "ai-diagnose-test", "pod": "p1"},
             "result": {"pod_name": "p1", "phase": "Running", "containers": [
                 {"name": "busybox", "state": "Waiting (CrashLoopBackOff)", "restart_count": 410,
                  "has_last_termination": True, "last_termination_reason": "Error", "last_termination_exit_code": 1}]}},
            {"tool": "get_pod_events", "arguments": {"namespace": "ai-diagnose-test", "pod": "p1"},
             "result": [{"reason": "BackOff", "message": "Back-off restarting failed container busybox"}]},
            {"tool": "get_container_logs", "arguments": {"namespace": "ai-diagnose-test", "pod": "p1", "container": "busybox"},
             "result": {"lines": ["Application starting...", "Application crashed"]}},
        ]
        text, diag = self._fmt(evidence)
        self.assertIn("exit code 1", text)
        self.assertIn("CrashLoopBackOff", text)
        self.assertIn("restart", text.lower())
        self.assertIn("BackOff", text)
        self.assertIn("Application crashed", text)
        self.assertIn("Root Cause:", text)
        # E: must not invent a specific exception
        self.assertNotIn("runtime exception", text.lower())
        self.assertNotIn("database failure", text.lower())
        self.assertNotIn("OOM", diag["diagnosis"])

    def test_15_imagepullbackoff_diagnosis_not_regressed(self):
        """F: ImagePullBackOff still produces its specific diagnosis."""
        evidence = [
            {"tool": "get_pod_status", "arguments": {"namespace": "d", "pod": "p"},
             "result": {"pod_name": "p", "containers": [
                 {"name": "app", "state": "Waiting (ImagePullBackOff)", "restart_count": 0,
                  "has_last_termination": False}]}},
        ]
        text, diag = self._fmt(evidence)
        self.assertIn("image could not be pulled", text)

    def test_16_oomkilled_diagnosis_not_regressed(self):
        """F: OOMKilled still produces its specific diagnosis."""
        evidence = [
            {"tool": "get_pod_status", "arguments": {"namespace": "d", "pod": "p"},
             "result": {"pod_name": "p", "containers": [
                 {"name": "app", "state": "Terminated", "restart_count": 3,
                  "has_last_termination": True, "last_termination_reason": "OOMKilled", "last_termination_exit_code": 137}]}},
        ]
        text, diag = self._fmt(evidence)
        self.assertIn("OOMKilled", text)
        self.assertIn("memory limit", text)

    def test_17_backoff_without_logs_flags_missing_evidence(self):
        """Proven vs hypothesis: BackOff without logs must not claim a specific cause."""
        evidence = [
            {"tool": "get_pod_events", "arguments": {"namespace": "d", "pod": "p"},
             "result": [{"reason": "BackOff", "message": "Back-off restarting failed container app"}]},
        ]
        text, diag = self._fmt(evidence)
        self.assertIn("BackOff", text)
        self.assertNotIn("runtime exception", text.lower())


    def test_18_node_health_question_uses_node_status(self):
        """Node/resource question: get_pod_status → discover node → get_node_status; no events/logs; no CrashLoop hijack."""
        namespace = "ai-diagnose-test"
        pod_name = "crashloop-test-896754f5f-n9rmf"
        node_name = "s8s-worker-eaf87a3e"

        class MockBridge:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                if tool_name == "get_pod_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "pod_name": pod_name, "namespace": namespace, "node_name": node_name,
                        "containers": [{"name": "busybox", "state": "Waiting (CrashLoopBackOff)",
                                        "restart_count": 410, "has_last_termination": True,
                                        "last_termination_reason": "Error", "last_termination_exit_code": 1}]}}
                if tool_name == "get_node_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "name": node_name, "status": "Ready",
                        "resources": {"cpu_capacity": "4", "cpu_allocatable": "3800m",
                                      "memory_capacity": "16Gi", "memory_allocatable": "14Gi"},
                        "system_info": {"kubelet_version": "v1.35.0", "operating_system": "linux",
                                        "architecture": "amd64", "kernel_version": "6.8", "container_runtime": "containerd"}}}
                return {"tool": tool_name, "arguments": arguments, "result": {}}

        mock_bridge = MockBridge()
        captured = {}
        diagnoser = self._make_bare_diagnoser(captured)

        res = diagnoser.diagnose(
            "What is the health and resource status of the node running this pod?",
            bridge=mock_bridge,
            namespace=namespace,
            pod=pod_name,
        )

        tools = [c["tool"] for c in mock_bridge.calls]
        self.assertEqual(tools[0], "get_pod_status")
        self.assertIn("get_node_status", tools)
        self.assertNotIn("get_pod_events", tools)
        self.assertNotIn("get_container_logs", tools)
        node_calls = [c for c in mock_bridge.calls if c["tool"] == "get_node_status"]
        self.assertEqual(node_calls[0]["arguments"]["node_name"], node_name)
        diag = res["final_diagnosis"]
        self.assertIn("Ready", diag)
        self.assertIn("CPU", diag)
        self.assertNotIn("CrashLoopBackOff", diag.split("Evidence:")[0])


    def test_19_workload_status_never_namespace_only(self):
        """get_workload_status must receive pod (or workload kind/name) when a target pod is known."""
        namespace = "ai-diagnose-test"
        pod_name = "crashloop-test-896754f5f-n9rmf"

        class MockBridge:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append({"tool": tool_name, "arguments": dict(arguments)})
                if tool_name == "get_workload_status":
                    # Fail the same way the real dispatcher does when args are insufficient.
                    if "pod" not in arguments and "workload_name" not in arguments:
                        return {"tool": tool_name, "arguments": arguments,
                                "error": "provider error [RESOURCE_NOT_FOUND]: workload not specified"}
                    return {"tool": tool_name, "arguments": arguments,
                            "result": {"pod_name": pod_name, "owner_references": [
                                {"kind": "ReplicaSet", "name": "crashloop-test-896754f5f", "controller": True}]}}
                return {"tool": tool_name, "arguments": arguments, "result": {"containers": []}}

        mock_bridge = MockBridge()
        diagnoser = self._make_bare_diagnoser()
        diagnoser.diagnose(
            "Why is this deployment not becoming healthy?",
            bridge=mock_bridge,
            namespace=namespace,
            pod=pod_name,
        )

        wl_calls = [c for c in mock_bridge.calls if c["tool"] == "get_workload_status"]
        self.assertTrue(wl_calls, "get_workload_status not called")
        for c in wl_calls:
            args = c["arguments"]
            has_specifics = ("pod" in args and args["pod"]) or ("workload_name" in args and args["workload_name"])
            self.assertTrue(has_specifics, f"get_workload_status called with insufficient args: {args}")
        self.assertEqual(wl_calls[0]["arguments"].get("pod"), pod_name)


    def _run_intent(self, question, mock_bridge):
        diagnoser = self._make_bare_diagnoser()
        return diagnoser.diagnose(question, bridge=mock_bridge, namespace="ai-diagnose-test", pod="p1")

    def _intent_bridge(self):
        class B:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append(tool_name)
                if tool_name == "get_pod_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "pod_name": "p1", "namespace": "ai-diagnose-test", "node_name": "node-1",
                        "containers": [{"name": "c", "state": "Waiting (CrashLoopBackOff)",
                                        "restart_count": 5, "has_last_termination": True,
                                        "last_termination_reason": "Error", "last_termination_exit_code": 1}]}}
                if tool_name == "get_gpu_status":
                    return {"tool": tool_name, "arguments": arguments,
                            "result": {"node_name": "node-1", "gpu_available": False, "gpu_resource_exposed": False}}
                if tool_name == "get_storage_status":
                    return {"tool": tool_name, "arguments": arguments,
                            "result": {"pod_name": "p1", "claims": [{"name": "data", "namespace": "ai-diagnose-test", "phase": "Bound", "storage_class_name": "longhorn", "requested_storage": "10Gi", "access_modes": ["RWO"]}]}}
                if tool_name == "get_service_status":
                    return {"tool": tool_name, "arguments": arguments,
                            "result": [{"name": "web", "type": "ClusterIP", "cluster_ip": "10.0.0.5", "subsets": [{"ready_address_count": 2, "not_ready_address_count": 0}]}]}
                if tool_name == "get_configuration_status":
                    return {"tool": tool_name, "arguments": arguments,
                            "result": {"pod_name": "p1", "service_account": {"name": "default"}, "config_maps": [{"name": "app-config", "keys": ["LOG_LEVEL"]}], "secrets": [], "image_pull_secrets": []}}
                return {"tool": tool_name, "arguments": arguments, "result": {}}
        return B()

    def test_20_gpu_intent(self):
        b = self._intent_bridge()
        res = self._run_intent("What is the GPU status of the node running this pod?", b)
        self.assertEqual(b.calls, ["get_pod_status", "get_gpu_status"])
        self.assertIn("GPU", res["final_diagnosis"])
        title = res["structured_diagnosis"]["diagnosis"]
        self.assertNotIn("CrashLoop", title)

    def test_21_storage_intent(self):
        b = self._intent_bridge()
        res = self._run_intent("What is the storage status for this pod?", b)
        self.assertIn("get_storage_status", b.calls)
        self.assertNotIn("get_pod_events", b.calls)
        self.assertIn("PVC", res["final_diagnosis"])

    def test_22_service_intent(self):
        b = self._intent_bridge()
        res = self._run_intent("What is the service and network status associated with this pod?", b)
        self.assertIn("get_service_status", b.calls)
        self.assertNotIn("get_container_logs", b.calls)
        self.assertIn("Service", res["final_diagnosis"])

    def test_23_config_intent(self):
        b = self._intent_bridge()
        res = self._run_intent("What configuration and identity settings are associated with this pod?", b)
        self.assertIn("get_configuration_status", b.calls)
        self.assertIn("service account", res["final_diagnosis"].lower())

    def test_24_crash_intent_preserved(self):
        b = self._intent_bridge()
        res = self._run_intent("Why is this pod repeatedly failing?", b)
        self.assertIn("get_pod_status", b.calls)
        self.assertIn("get_pod_events", b.calls)
        self.assertIn("get_container_logs", b.calls)
        self.assertIn("exit code 1", res["final_diagnosis"])


    def test_25_empty_service_evidence_not_crashloop(self):
        """get_service_status returns [] → answer must be service-focused, never CrashLoop."""
        namespace = "ai-diagnose-test"
        pod_name = "crashloop-test-896754f5f-n9rmf"

        class B:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append(tool_name)
                if tool_name == "get_pod_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "pod_name": pod_name, "namespace": namespace, "node_name": "node-1",
                        "containers": [{"name": "busybox", "state": "Waiting (CrashLoopBackOff)",
                                        "restart_count": 410, "has_last_termination": True,
                                        "last_termination_reason": "Error", "last_termination_exit_code": 1}]}}
                if tool_name == "get_service_status":
                    return {"tool": tool_name, "arguments": arguments, "result": []}
                return {"tool": tool_name, "arguments": arguments, "result": {}}

        b = B()
        diagnoser = self._make_bare_diagnoser()
        res = diagnoser.diagnose(
            "What is the service and network status associated with this pod?",
            bridge=b, namespace=namespace, pod=pod_name,
        )
        text = res["final_diagnosis"]
        title = res["structured_diagnosis"]["diagnosis"]
        self.assertIn("get_service_status", b.calls)
        self.assertIn("No services", text)
        self.assertNotIn("CrashLoopBackOff", title)
        self.assertNotIn("exit code 1", title)

    def test_26_gpu_empty_or_false_not_crashloop(self):
        b = self._intent_bridge()
        res = self._run_intent("What is the GPU status of the node running this pod?", b)
        text = res["final_diagnosis"]
        title = res["structured_diagnosis"]["diagnosis"]
        self.assertIn("GPU", text)
        self.assertNotIn("exit code 1", title)


    def _pending_bridge(self, event_reason, event_msg, pod_name="p1"):
        class B:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append(tool_name)
                if tool_name == "get_pod_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "pod_name": pod_name, "namespace": "ai-diagnose-test",
                        "phase": "Pending", "node_name": "<not yet scheduled>",
                        "containers": [{"name": "nginx", "state": "no status available",
                                        "restart_count": 0, "has_last_termination": False}]}}
                if tool_name == "get_pod_events":
                    return {"tool": tool_name, "arguments": arguments,
                            "result": [{"reason": event_reason, "message": event_msg}]}
                if tool_name == "get_container_logs":
                    return {"tool": tool_name, "arguments": arguments, "result": {"lines": []}}
                return {"tool": tool_name, "arguments": arguments, "result": {}}
        return B()

    def test_27_pending_failedscheduling_no_logs_and_scheduling_diagnosis(self):
        b = self._pending_bridge("FailedScheduling", "0/3 nodes are available: 3 Insufficient cpu.")
        diagnoser = self._make_bare_diagnoser()
        res = diagnoser.diagnose(
            "Why is pod p1 not starting?", bridge=b,
            namespace="ai-diagnose-test", pod="p1",
        )
        self.assertIn("get_pod_status", b.calls)
        self.assertIn("get_pod_events", b.calls)
        self.assertNotIn("get_container_logs", b.calls)
        text = res["final_diagnosis"].lower()
        self.assertIn("pending", text)
        self.assertIn("scheduled", text)
        self.assertIn("insufficient cpu", text)

    def test_28_pending_insufficient_memory(self):
        b = self._pending_bridge("FailedScheduling", "0/3 nodes are available: 3 Insufficient memory.")
        diagnoser = self._make_bare_diagnoser()
        res = diagnoser.diagnose(
            "Why is pod p1 not starting?", bridge=b,
            namespace="ai-diagnose-test", pod="p1",
        )
        self.assertNotIn("get_container_logs", b.calls)
        self.assertIn("insufficient memory", res["final_diagnosis"].lower())

    def test_29_running_crashloop_still_uses_logs(self):
        class B:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append(tool_name)
                if tool_name == "get_pod_status":
                    return {"tool": tool_name, "arguments": arguments, "result": {
                        "pod_name": "p1", "namespace": "ai-diagnose-test",
                        "phase": "Running", "node_name": "node-1",
                        "containers": [{"name": "c", "state": "Waiting (CrashLoopBackOff)",
                                        "restart_count": 5, "has_last_termination": True,
                                        "last_termination_reason": "Error", "last_termination_exit_code": 1}]}}
                return {"tool": tool_name, "arguments": arguments, "result": {"lines": ["x"]}}
        b = B()
        diagnoser = self._make_bare_diagnoser()
        diagnoser.diagnose(
            "Why is this pod repeatedly failing?", bridge=b,
            namespace="ai-diagnose-test", pod="p1",
        )
        self.assertIn("get_container_logs", b.calls)

    def test_30_arbitrary_pending_pod_name(self):
        b = self._pending_bridge("FailedScheduling", "0/3 nodes: 3 node(s) had untolerated taint.", pod_name="some-totally-different-pod-name")
        diagnoser = self._make_bare_diagnoser()
        res = diagnoser.diagnose(
            "Why is this pod failing to start and what do the logs say?", bridge=b,
            namespace="prod-x", pod="some-totally-different-pod-name",
        )
        self.assertNotIn("get_container_logs", b.calls)
        self.assertIn("taint", res["final_diagnosis"].lower())


    def _pending_evidence(self, node, containers, events):
        return [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "p1", "phase": "Pending", "node_name": node, "containers": containers}},
            {"tool": "get_pod_events", "arguments": {}, "result": events},
        ]

    def test_31_pending_no_node_insufficient_cpu(self):
        ev = self._pending_evidence("<not yet scheduled>", [], [
            {"reason": "FailedScheduling", "message": "0/3 nodes are available: 3 Insufficient cpu."}])
        text, diag = self._fmt(ev)
        self.assertIn("cannot be scheduled", diag["diagnosis"])
        self.assertIn("Insufficient cpu", text)

    def test_32_pending_no_node_selector_mismatch(self):
        ev = self._pending_evidence("<not yet scheduled>", [], [
            {"reason": "FailedScheduling", "message": "0/3 nodes are available: 3 node(s) didn't match Pod's node affinity/selector."}])
        text, diag = self._fmt(ev)
        self.assertIn("cannot be scheduled", diag["diagnosis"])
        self.assertIn("selector", text.lower())

    def test_33_pending_scheduled_create_config_error(self):
        ev = self._pending_evidence(
            "s8s-worker-eaf87a3e",
            [{"name": "nginx", "state": 'Waiting (CreateContainerConfigError: configmap "does-not-exist-config" not found)'}],
            [
                {"reason": "Scheduled", "message": "Successfully assigned the pod to the node"},
                {"reason": "Failed", "message": 'Error: configmap "does-not-exist-config" not found'},
            ],
        )
        text, diag = self._fmt(ev)
        self.assertNotIn("cannot be scheduled", diag["diagnosis"])
        self.assertIn("configuration dependency", diag["diagnosis"].lower())
        self.assertIn("does-not-exist-config", text)

    def test_34_pending_scheduled_image_pull_failure(self):
        ev = self._pending_evidence(
            "s8s-worker-eaf87a3e",
            [{"name": "nginx", "state": "Waiting (ErrImagePull: rpc error: image not found)"}],
            [{"reason": "Scheduled", "message": "Successfully assigned the pod to the node"}],
        )
        text, diag = self._fmt(ev)
        self.assertNotIn("cannot be scheduled", diag["diagnosis"])
        self.assertIn("image", diag["diagnosis"].lower())

    def test_35_pending_scheduled_other_wait(self):
        ev = self._pending_evidence(
            "s8s-worker-eaf87a3e",
            [{"name": "nginx", "state": "Waiting (ContainerCreating: pulling image)"}],
            [{"reason": "Scheduled", "message": "Successfully assigned the pod to the node"}],
        )
        text, diag = self._fmt(ev)
        self.assertNotIn("cannot be scheduled", diag["diagnosis"])
        self.assertIn("ContainerCreating", text)


    def test_36_liveness_probe_failure_is_causal(self):
        ev = [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "p", "phase": "Running", "node_name": "n", "containers": [
                    {"name": "app", "state": "Waiting (CrashLoopBackOff)", "restart_count": 8,
                     "has_last_termination": True, "last_termination_reason": "Error", "last_termination_exit_code": 1}]}},
            {"tool": "get_pod_events", "arguments": {}, "result": [
                {"reason": "Unhealthy", "message": "Liveness probe failed: HTTP probe failed with statuscode: 404"}]},
        ]
        text, diag = self._fmt(ev)
        self.assertIn("liveness probe", diag["diagnosis"])
        self.assertNotIn("exit code 1", diag["diagnosis"])

    def test_37_readiness_failure_not_restart(self):
        ev = [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "p", "phase": "Running", "node_name": "n", "containers": [
                    {"name": "app", "state": "Running", "restart_count": 0}]}},
            {"tool": "get_pod_events", "arguments": {}, "result": [
                {"reason": "Unhealthy", "message": "Readiness probe failed: Get /healthz: dial tcp refused"}]},
        ]
        text, diag = self._fmt(ev)
        self.assertIn("readiness", diag["diagnosis"].lower())
        self.assertIn("endpoints", text)
        self.assertNotIn("restarted because", diag["diagnosis"].lower())

    def test_38_completed_is_not_failure(self):
        ev = [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "job-1", "phase": "Succeeded", "node_name": "n", "containers": [
                    {"name": "worker", "state": "Terminated (reason: Completed, exit: 0)", "restart_count": 0,
                     "has_last_termination": True, "last_termination_reason": "Completed", "last_termination_exit_code": 0}]}},
        ]
        text, diag = self._fmt(ev)
        self.assertIn("completed successfully", diag["diagnosis"])
        self.assertNotIn("CrashLoop", text)

    def test_39_init_container_failure_is_init_not_app(self):
        ev = [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "p", "phase": "Pending", "node_name": "n",
                "containers": [{"name": "app", "state": "no status available", "restart_count": 0}],
                "init_containers": [{"name": "wait-for-db", "state": "Terminated (reason: Error, exit: 1)",
                                     "restart_count": 3, "has_last_termination": True,
                                     "last_termination_reason": "Error", "last_termination_exit_code": 1}]}},
        ]
        text, diag = self._fmt(ev)
        self.assertIn("Init container", text)
        self.assertIn("wait-for-db", text)

    def test_40_multi_container_sidecar_failure(self):
        ev = [
            {"tool": "get_pod_status", "arguments": {}, "result": {
                "pod_name": "p", "phase": "Running", "node_name": "n", "containers": [
                    {"name": "app", "state": "Running", "restart_count": 0},
                    {"name": "sidecar", "state": "Waiting (CrashLoopBackOff)", "restart_count": 12,
                     "has_last_termination": True, "last_termination_reason": "Error", "last_termination_exit_code": 1}]}},
        ]
        text, diag = self._fmt(ev)
        self.assertIn("sidecar", text)
        self.assertNotIn("Container 'app'", text.split("Evidence:")[0])


    def test_41_missing_target_stops_and_states_not_found(self):
        namespace = "ai-diagnose-test"
        pod_name = "completely-made-up-pod-name"

        class B:
            provider_type = "mock"

            def __init__(self):
                self.calls = []

            def execute_tool(self, tool_name, arguments):
                self.calls.append(tool_name)
                return {"tool": tool_name, "arguments": arguments,
                        "error": 'provider error [RESOURCE_NOT_FOUND]: pod "completely-made-up-pod-name" not found in namespace "ai-diagnose-test"'}

        b = B()
        diagnoser = self._make_bare_diagnoser()
        res = diagnoser.diagnose(
            "Why is this pod repeatedly failing?", bridge=b,
            namespace=namespace, pod=pod_name,
        )
        self.assertEqual(b.calls, ["get_pod_status"], f"dependent tools executed: {b.calls}")
        text = res["final_diagnosis"].lower()
        self.assertIn("could not be found", text)
        title = res["structured_diagnosis"]["diagnosis"].lower()
        self.assertNotIn("workload failure", title)
        self.assertNotIn("crash", title)
        self.assertNotIn("exit code", title)


if __name__ == "__main__":
    unittest.main()
