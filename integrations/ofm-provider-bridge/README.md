# OFM Provider Bridge → CLIProxyAPI v8

**Status: POC / NOT PRODUCTION-READY.** Standalone integration workspace inside the owner's CLIProxyAPI fork. This fork's main branch is dated 2026-05-18 and does not track the reviewed 2026-11-13 v8 host. Do not merge or deploy until a clean current-v8 integration branch is established and end-to-end tests pass.

## Verified source baselines
- DSH: Ebony-Vinyl/dsh-our-free-model at 9aa84122 (2026-11-11); package v2.8.3.
- CPA: router-for-me/CLIProxyAPI at 1aaf56bc (2026-11-13); Go module /v8; ABI 1; RPC schema 10 in main.
- DSH registry: 76 providers / 507 model records. Formats: 70 chat, 3 messages, 3 responses. Auth: 69 apiKey, 3 none, 2 OAuth, 2 ADC. **The intersection of chat and apiKey is only 64 providers.**
- The Azure OpenAI row among those 64 has no concrete base URL and is skipped by our safe generator. Thus the generator should emit 63 *candidate* disabled groups from that exact registry, subject to review.

## Two complementary deliverables

### C1: generator (standard upstreams, host-owned)
- tools/registry_to_cpa.py accepts a DSH registry JSON and emits **disabled**, keyless CPA v8 openai-compatibility provider entries and a separate machine-readable skip manifest.
- Only format=openai-chat AND auth.kind=apiKey AND valid HTTPS base URLs are candidates. Other wires and OAuth/ADC are **not** silently coerced into Bearer chat.
- All generated entries have disabled:true and keys:[]. The operator reviews each model, endpoint, provider's authorization, and retry classification; then supplies valid credentials through their secure deployment process.
- We do **not** implement account-client imitation, free-tier bypass, proxy rotation to evade rate/region enforcement, or unofficial login flows.

Example:
~~~
python3 integrations/ofm-provider-bridge/tools/registry_to_cpa.py \
  --registry /path/to/dsh-our-free-model/src/providers/registry.json \
  --output /tmp/ofm-providers.v8.yaml \
  --report /tmp/ofm-provider-review.json
~~~
**Warning:** This is a snippet containing api-keys: at the root. Merge with existing CPA config; do not replace existing client access.api-keys or production credentials. The preview is deliberately non-runnable until each operator enables the reviewed providers.

### C2: plugin prototype (only explicitly authorized OpenAI-chat upstream)
- plugin/main.go is an isolated no-external-Go-package c-shared plugin implementation using the reviewed C ABI, abi_version=1, schema_version=1.
- Registers its own model namespace from OFM_AUTHORIZED_MODELS. Executes ordinary non-streaming Chat Completions through host.http.do; streams via host.http.do_stream → host.http.stream_read → host.stream.emit → host.stream.close.
- Requires your own upstream authorization, HTTPS endpoint, and API key; it does not connect to DSH's anonymous or account channels.
- Blocks unconfigured/unknown models, sets explicit HTTP error status, avoids returning upstream errors containing credentials, and fails closed for missing [DONE].
- **Unverified:** host version schema negotiation, zero-auth routing, full SSE behavior, concurrent stream cancel semantics, and production transport. DO NOT deploy yet.

Build (Linux / CGO and C compiler required):
~~~
cd integrations/ofm-provider-bridge/plugin
CGO_ENABLED=1 GOAMD64=v1 go build -buildmode=c-shared -o ofm-authorized.so .
nm -D ofm-authorized.so | grep cliproxy_plugin_init
~~~
Example sandbox environment (never commit secrets):
~~~
export OFM_AUTHORIZED_BASE_URL=https://YOUR_AUTHORIZED_PROVIDER/v1
export OFM_AUTHORIZED_API_KEY=YOUR_AUTHORIZED_TOKEN
export OFM_AUTHORIZED_MODELS=model-one,org/model-two
~~~
Configure the host (v8 target, not the stale fork):
~~~
plugins:
  enabled: true
  dir: "plugins"
  configs:
    ofm-authorized:
      enabled: true
      priority: 91
~~~
Plugin filename basename must match the plugin id: plugins/ofm-authorized.so. Model aliases are ofm-authorized/<upstream-id>. Do not enable until the P0 gates below pass.

## P0 gates
1. Refresh a standalone test host to the current v8 release; do not modify production and do not assume fork main is compatible.
2. Verify ABI registration and target-image schema negotiation. Confirm actual capabilities, not only plugin.register success.
3. Prove authenticated /v1/models, non-streaming, streaming; test additionally with **zero upstream auth** and record whether CPA blocks at the selector (auth_not_found).
4. Strict downstream auth; test 401, 403, 404, 429, 5xx and Retry-After classification; prevent unexpected automatic retry.
5. Test concurrency >=8, missing [DONE], stream disconnect, emit failure, upstream and downstream close failure, and memory ownership.
6. Confirm upstream authorization and terms before enabling any individual lane. Keep anonymous DSH-specific free-lane emulation out of scope.
7. Validate release artifacts on the target OS/CPU/libc; in-process native crashes can crash the host. Canary only with rollback.

## Mapping

| DSH source | Destination |
| --- | --- |
| src/providers/registry.json and index.js | C1 converter → native api-keys.openai-compatibility; not runtime plugin |
| src/aggregate.js resolveProvider | Host credential/config resolution, per-provider group |
| src/aggregate.js postCompatibleStreamed | Host native chat executor; specialized wires need separate executor |
| ctx.llm.registerAdapter / FreeModelAdapter | C2 plugin.register + model.register + executor.identifier |
| FreeModelAdapter.prepareCall / stream | C2 executor.execute / execute_stream |
| src/forward.js listener | Omitted; use CPA built-in API routes |
| src/forward.js protocol shaping | Compare host translator before dropping; not assumed equivalent |
| combos fallback | Native alias pool only partially equivalent; ordered failover requires new routing work |
| EAC/Kilo/account channels | Deferred, vendor-authorized adapter or sidecar; no automatic migration |
| dsh settings.json | Host config and credential store; no secrets committed |

## Test commands
~~~
python3 -m unittest discover -s integrations/ofm-provider-bridge/tests -v
~~~

## Sources
- https://github.com/Ebony-Vinyl/dsh-our-free-model/blob/9aa84122/src/providers/index.js
- https://github.com/Ebony-Vinyl/dsh-our-free-model/blob/9aa84122/src/aggregate.js
- https://github.com/router-for-me/CLIProxyAPI/blob/1aaf56bc/sdk/pluginabi/types.go
- https://github.com/router-for-me/CLIProxyAPI/blob/1aaf56bc/examples/plugin/executor/go/main.go
- https://help.router-for.me/plugin/model-registrar

## License and provenance
This workspace contains independently written integration code. No DSH vendored JS/WASM was copied; when importing any code/data later, review all original MIT LICENSE and NOTICE records and attribution and respect provider terms.
