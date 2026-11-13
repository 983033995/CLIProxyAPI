#!/usr/bin/env python3
"""Convert DSH chat/apiKey providers to disabled CLIProxyAPI v8 groups.
No credentials are read or emitted; review providers before enabling.
"""
import argparse
import json
import re
from pathlib import Path
from urllib.parse import urlsplit

DENIED = re.compile(r"workbuddy|codebuddy|huaweicloud|huawei[\s-]*cloud|modelarts|codearts|华为云|码道", re.I)

def quote(value):
    return json.dumps(str(value), ensure_ascii=False)

def allowed_url(value):
    if not isinstance(value, str) or any(c in value for c in "<>{}$"):
        return False
    parsed = urlsplit(value.strip())
    return parsed.scheme == "https" and bool(parsed.hostname) and not parsed.username and not parsed.password

def convert(registry, namespace="ofm-"):
    """Return safe YAML and a machine-readable review manifest."""
    if not isinstance(registry, list):
        raise ValueError("registry root must be an array")
    lines = ["# Generated from DSH registry. ALL groups disabled with empty keys.", "# Merge manually; do not overwrite your existing api-keys section.", "api-keys:", "  openai-compatibility:"]
    selected, skipped, names = [], [], set()
    for p in registry:
        pid = str(p.get("id", "")).strip()
        auth = p.get("auth") or {}
        fmt = p.get("format")
        if not pid or not re.fullmatch(r"[a-zA-Z0-9_-]+", pid) or pid in names:
            raise ValueError("invalid or duplicate provider id: " + pid)
        names.add(pid)
        why = None
        if DENIED.search(pid + " " + str(p.get("name", "")) + " " + str(p.get("notes", "")) + " " + str(p.get("baseUrl", ""))):
            why = "upstream-excluded"
        elif auth.get("kind") != "apiKey":
            why = "non-apikey"
        elif fmt != "openai-chat":
            why = "non-chat-format"
        elif not allowed_url(p.get("baseUrl")):
            why = "missing-or-unsafe-base-url"
        if why:
            skipped.append({"id": pid, "reason": why, "format": fmt, "auth": auth.get("kind")})
            continue
        models, model_ids = [], set()
        for m in p.get("models") or []:
            mid = m.get("id") if isinstance(m, dict) else None
            if not isinstance(mid, str) or not mid.strip() or mid in model_ids:
                continue
            model_ids.add(mid)
            models.append(m)
        if not models:
            skipped.append({"id": pid, "reason": "no-models"})
            continue
        route_prefix = namespace + pid
        selected.append({"id": pid, "prefix": route_prefix, "models": len(models), "baseUrl": p["baseUrl"]})
        lines += [
            "    - name: " + quote(pid),
            "      disabled: true",
            "      prefix: " + quote(route_prefix),
            "      base-url: " + quote(p["baseUrl"].rstrip("/")),
            "      request-retry: 0",
            "      keys: []  # Supply authorized key securely before enabling",
            "      models:",
        ]
        for m in models:
            lines += [
                "        - name: " + quote(m["id"]),
                "          alias: " + quote(m["id"]),
                "          display-name: " + quote(m.get("name") or m["id"]),
            ]
            context = m.get("contextWindow")
            if isinstance(context, int) and not isinstance(context, bool) and context > 0:
                lines.append("          max-context-length: " + str(context))
            if m.get("vision") is True:
                lines.append("          input-modalities: [text, image]")
    if not selected:
        lines.append("    []")
    return "\n".join(lines) + "\n", {
        "selected": selected, "skipped": skipped, "selectedCount": len(selected),
        "skippedCount": len(skipped), "modelCount": sum(p["models"] for p in selected),
        "note": "Never auto-enable these groups. Wire, URL, credentials and terms require separate review."
    }

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--registry", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--report", required=True, type=Path)
    parser.add_argument("--namespace", default="ofm-")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9_-]*", args.namespace):
        parser.error("namespace accepts only letters, numbers, underscore and hyphen")
    data = json.loads(args.registry.read_text(encoding="utf-8"))
    yaml_text, manifest = convert(data, args.namespace)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(yaml_text, encoding="utf-8")
    args.report.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("Generated %d disabled groups / %d models; skipped %d." % (
        manifest["selectedCount"], manifest["modelCount"], manifest["skippedCount"]))

if __name__ == "__main__":
    main()
