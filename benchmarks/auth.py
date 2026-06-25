"""Resolve authHelper tokens on the host before injecting into container config."""

import json
import subprocess
from typing import Any


def resolve_auth_tokens(config: dict[str, Any]) -> dict[str, Any]:
    providers = config.get("provider", {}).get("providers", [])
    kept = []
    for p in providers:
        auth_helper = p.pop("authHelper", None)
        auth_mode = p.get("authMode", "")

        if auth_mode == "bearer" and auth_helper:
            try:
                token = subprocess.run(
                    auth_helper, shell=True, capture_output=True, text=True, timeout=15,
                ).stdout.strip()
                if token:
                    p["apiKey"] = token
                    p["authMode"] = "apikey"
                    kept.append(p)
                    continue
            except Exception:
                pass

        key = p.get("apiKey") or p.get("apikey") or p.get("APIKey", "")
        if key:
            p["apiKey"] = key
            p.pop("apikey", None)
            p.pop("APIKey", None)
            kept.append(p)
            continue

        # Keep providers that don't require auth (e.g., traecli with internal auth)
        kept.append(p)

    config["provider"]["providers"] = kept
    return config