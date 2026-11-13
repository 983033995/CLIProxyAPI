package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);
typedef struct {
  uint32_t abi_version; void* host_ctx;
  cliproxy_host_call_fn call; cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;
typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);
typedef struct {
  uint32_t abi_version; cliproxy_plugin_call_fn call;
  cliproxy_plugin_free_fn free_buffer; cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;
extern int ofmPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void ofmPluginFree(void*, size_t);
extern void ofmPluginShutdown(void);
static const cliproxy_host_api* stored_host;
static void store_host(const cliproxy_host_api* host) { stored_host = host; }
static int invoke_host(const char* name, const uint8_t* data, size_t len, cliproxy_buffer* response) {
  if (!stored_host || !stored_host->call) return 1;
  return stored_host->call(stored_host->host_ctx, name, data, len, response);
}
static void release_host(void* ptr, size_t len) {
  if (ptr && stored_host && stored_host->free_buffer) stored_host->free_buffer(ptr, len);
}
*/
import "C"

import (
  "bytes"
  "encoding/json"
  "errors"
  "fmt"
  "net/http"
  "net/url"
  "os"
  "strings"
  "unsafe"
)

const pluginID = "ofm-authorized"
const modelPrefix = pluginID + "/"
const maxRPC = 16 << 20

type envelope struct {
  OK bool `json:"ok"`
  Result json.RawMessage `json:"result,omitempty"`
  Error *pluginError `json:"error,omitempty"`
}
type pluginError struct {
  Code string `json:"code"`
  Message string `json:"message"`
  HTTPStatus int `json:"http_status,omitempty"`
}
type executorRequest struct {
  Model string
  Payload []byte
  StreamID string `json:"stream_id"`
  HostCallbackID string `json:"host_callback_id"`
}
type httpResponse struct {
  StatusCode int
  Headers http.Header
  Body []byte
}
type httpStreamResponse struct {
  StatusCode int `json:"status_code"`
  StreamID string `json:"stream_id"`
}
type httpStreamChunk struct {
  Payload []byte `json:"payload"`
  Done bool `json:"done"`
  Error string `json:"error"`
}
type modelInfo struct {
  ID string
  Object string
  OwnedBy string
  DisplayName string
  SupportedGenerationMethods []string
  UserDefined bool
}
type upstream struct {
  BaseURL string
  Key string
  Models map[string]bool
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
  if host == nil || plugin == nil || host.abi_version != 1 { return 1 }
  C.store_host(host)
  plugin.abi_version = 1
  plugin.call = C.cliproxy_plugin_call_fn(C.ofmPluginCall)
  plugin.free_buffer = C.cliproxy_plugin_free_fn(C.ofmPluginFree)
  plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.ofmPluginShutdown)
  return 0
}

//export ofmPluginCall
func ofmPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
  if response == nil { return 1 }
  response.ptr = nil
  response.len = 0
  if method == nil || uint64(requestLen) > maxRPC {
    writeResponse(response, fail("invalid_request", "invalid plugin call", 400))
    return 1
  }
  var raw []byte
  if request != nil && requestLen > 0 { raw = C.GoBytes(unsafe.Pointer(request), C.int(requestLen)) }
  output := dispatch(C.GoString(method), raw)
  writeResponse(response, output)
  return 0
}

//export ofmPluginFree
func ofmPluginFree(ptr unsafe.Pointer, _ C.size_t) { if ptr != nil { C.free(ptr) } }

//export ofmPluginShutdown
func ofmPluginShutdown() {}

func writeResponse(out *C.cliproxy_buffer, value []byte) {
  if len(value) == 0 { return }
  out.ptr = C.CBytes(value)
  out.len = C.size_t(len(value))
}
func success(value any) []byte {
  r, _ := json.Marshal(value)
  b, _ := json.Marshal(envelope{OK: true, Result: r})
  return b
}
func fail(code, message string, status int) []byte {
  b, _ := json.Marshal(envelope{OK: false, Error: &pluginError{
    Code: code, Message: message, HTTPStatus: status,
  }})
  return b
}
func settings() (upstream, error) {
  cfg := upstream{BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("OFM_AUTHORIZED_BASE_URL")), "/"),
    Key: strings.TrimSpace(os.Getenv("OFM_AUTHORIZED_API_KEY")), Models: map[string]bool{}}
  u, err := url.Parse(cfg.BaseURL)
  if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
    return cfg, errors.New("OFM_AUTHORIZED_BASE_URL must be an HTTPS base URL")
  }
  if cfg.Key == "" { return cfg, errors.New("OFM_AUTHORIZED_API_KEY is required") }
  for _, name := range strings.Split(os.Getenv("OFM_AUTHORIZED_MODELS"), ",") {
    name = strings.TrimSpace(name)
    if name != "" { cfg.Models[name] = true }
  }
  if len(cfg.Models) == 0 { return cfg, errors.New("OFM_AUTHORIZED_MODELS must list permitted model ids") }
  return cfg, nil
}
func dispatch(method string, raw []byte) []byte {
  switch method {
  case "plugin.register", "plugin.reconfigure":
    return success(map[string]any{"schema_version": 1,
      "metadata": map[string]any{"Name": pluginID, "Version": "0.1.0-poc", "Author": "983033995",
        "ConfigFields": []any{}},
      "capabilities": map[string]any{"model_registrar": true, "executor": true,
        "executor_model_scope": "static",
        "executor_input_formats": []string{"chat-completions"},
        "executor_output_formats": []string{"chat-completions"}}})
  case "model.register":
    cfg, err := settings()
    models := []modelInfo{}
    if err == nil {
      for name := range cfg.Models { models = append(models, modelInfo{
        ID: modelPrefix + name, Object: "model", OwnedBy: pluginID, DisplayName: name,
        SupportedGenerationMethods: []string{"chat"}, UserDefined: true,
      }) }
    }
    return success(map[string]any{"Provider": pluginID, "Models": models})
  case "executor.identifier":
    return success(map[string]any{"identifier": pluginID})
  case "executor.execute", "executor.execute_stream":
    var req executorRequest
    if err := json.Unmarshal(raw, &req); err != nil {
      return fail("request_scoped", "invalid executor request", 400)
    }
    cfg, err := settings()
    if err != nil { return fail("not_implemented", "authorized upstream is not configured", 503) }
    name := strings.TrimPrefix(req.Model, modelPrefix)
    if !strings.HasPrefix(req.Model, modelPrefix) || !cfg.Models[name] {
      return fail("model_not_found", "unknown authorized model", 404)
    }
    payload, err := preparePayload(req.Payload, name, method == "executor.execute_stream")
    if err != nil { return fail("request_scoped", "invalid chat-completions payload", 400) }
    if method == "executor.execute" { return runOnce(req.HostCallbackID, cfg, payload) }
    if req.StreamID == "" { return fail("request_scoped", "missing downstream stream id", 400) }
    return runStream(req.HostCallbackID, req.StreamID, cfg, payload)
  case "executor.count_tokens":
    return fail("not_implemented", "token counting is not supported", 501)
  default:
    return fail("not_implemented", "unsupported method", 501)
  }
}
func preparePayload(source []byte, name string, stream bool) ([]byte, error) {
  var p map[string]any
  if err := json.Unmarshal(source, &p); err != nil || p == nil { return nil, errors.New("invalid payload") }
  p["model"] = name
  p["stream"] = stream
  return json.Marshal(p)
}
func hostCall(method string, request any, dest any) error {
  body, err := json.Marshal(request)
  if err != nil { return err }
  name := C.CString(method)
  defer C.free(unsafe.Pointer(name))
  var ptr *C.uint8_t
  if len(body) != 0 {
    ptr = (*C.uint8_t)(C.CBytes(body))
    defer C.free(unsafe.Pointer(ptr))
  }
  var result C.cliproxy_buffer
  status := C.invoke_host(name, ptr, C.size_t(len(body)), &result)
  var response []byte
  if result.ptr != nil {
    response = C.GoBytes(result.ptr, C.int(result.len))
    C.release_host(result.ptr, result.len)
  }
  if status != 0 { return fmt.Errorf("host callback failed") }
  var frame envelope
  if err := json.Unmarshal(response, &frame); err != nil { return err }
  if !frame.OK { return errors.New("host callback rejected request") }
  if dest == nil { return nil }
  return json.Unmarshal(frame.Result, dest)
}
func httpRequest(callbackID string, cfg upstream, payload []byte) map[string]any {
  return map[string]any{"host_callback_id": callbackID, "method": "POST",
    "url": cfg.BaseURL + "/chat/completions",
    "headers": http.Header{"Authorization": {"Bearer " + cfg.Key},
      "Content-Type": {"application/json"}, "Accept": {"text/event-stream, application/json"}},
    "body": payload}
}
func runOnce(callbackID string, cfg upstream, payload []byte) []byte {
  var resp httpResponse
  if err := hostCall("host.http.do", httpRequest(callbackID, cfg, payload), &resp); err != nil {
    return fail("transient_transport", "authorized upstream transport failed", 502)
  }
  if resp.StatusCode < 200 || resp.StatusCode >= 300 {
    return fail(classify(resp.StatusCode), "authorized upstream request failed", resp.StatusCode)
  }
  if !json.Valid(resp.Body) { return fail("stream_truncated", "upstream returned invalid JSON", 502) }
  return success(map[string]any{"Payload": resp.Body, "Headers": resp.Headers})
}
func classify(status int) string {
  switch status {
  case 400: return "request_scoped"
  case 401: return "invalid_api_key"
  case 403: return "request_scoped"
  case 404: return "model_not_found"
  case 429: return "transient_rate_limit"
  default: return "transient_transport"
  }
}
func runStream(callbackID, downstreamID string, cfg upstream, payload []byte) []byte {
  var start httpStreamResponse
  if err := hostCall("host.http.do_stream", httpRequest(callbackID, cfg, payload), &start); err != nil {
    return fail("transient_transport", "upstream streaming transport failed", 502)
  }
  if start.StreamID == "" { return fail("transient_transport", "upstream returned no stream id", 502) }
  if start.StatusCode < 200 || start.StatusCode >= 300 {
    _ = hostCall("host.http.stream_close", map[string]any{"stream_id": start.StreamID}, nil)
    return fail(classify(start.StatusCode), "upstream streaming request failed", start.StatusCode)
  }
  go pumpStream(start.StreamID, downstreamID)
  return success(map[string]any{"headers": http.Header{"Content-Type": {"text/event-stream"}}})
}
func pumpStream(upstreamID, downstreamID string) {
  defer func() { _ = hostCall("host.http.stream_close", map[string]any{"stream_id": upstreamID}, nil) }()
  closeWith := func(code, message string, status int) {
    detail := map[string]any{"code": code, "message": message, "http_status": status}
    _ = hostCall("host.stream.close", map[string]any{"stream_id": downstreamID, "error_detail": detail}, nil)
  }
  var trailer []byte
  hasDone := false
  for {
    var chunk httpStreamChunk
    if err := hostCall("host.http.stream_read", map[string]any{"stream_id": upstreamID}, &chunk); err != nil {
      closeWith("stream_truncated", "upstream stream read failed", 502); return
    }
    if chunk.Error != "" {
      closeWith("stream_truncated", "upstream stream interrupted", 502); return
    }
    if len(chunk.Payload) > 0 {
      trailer = append(trailer, chunk.Payload...)
      if bytes.Contains(trailer, []byte("data: [DONE]")) { hasDone = true }
      if len(trailer) > 128 { trailer = append([]byte{}, trailer[len(trailer)-128:]...) }
      if err := hostCall("host.stream.emit", map[string]any{"stream_id": downstreamID, "payload": chunk.Payload}, nil); err != nil {
        closeWith("connection_lifecycle", "downstream stream closed", 499); return
      }
    }
    if chunk.Done { break }
  }
  if !hasDone { closeWith("stream_truncated", "upstream stream ended without terminal frame", 502); return }
  _ = hostCall("host.stream.close", map[string]any{"stream_id": downstreamID}, nil)
}
