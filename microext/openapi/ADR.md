# OpenAPI Extension for NATS Micro Services

| Metadata | Value                    |
|----------|--------------------------|
| Date     | 2026-05-14               |
| Author   | @joeriddle               |
| Status   | Proposed                 |
| Tags     | micro, openapi, orbit.go |

| Revision | Date       | Author      | Info           |
|----------|------------|-------------|----------------|
| 1        | 2026-05-14 | @joeriddle  | Initial design |

## Context and Problem Statement

NATS micro services expose endpoints via subjects but lack a standard way to describe request/response schemas, parameters, and documentation metadata. How can we bring OpenAPI 3.1 specification support to NATS micro so that HTTP gateways can auto-discover endpoints and developer tooling can query services for documentation, code generation, and service catalog purposes?

## Prior Work

- [Huma](https://github.com/danielgtaylor/huma) — HTTP framework with typed handler registration (`Register[I, O any]`) and automatic OpenAPI schema generation from Go types via reflection. Inspiration for the two-tier API design.
- [huma-nats-micro prototype](https://github.com/synadia-io/huma-nats-micro) — manual integration between Huma and NATS micro that queries `micro.Service.Info()` to reconstruct HTTP routes from endpoint metadata.

## Design

### Spec Location

Fragments in endpoint metadata for lightweight auto-discovery, plus a dedicated endpoint serving the full assembled OpenAPI 3.1 document.

- Control subjects: `$SRV.INFO.<service>.$openapi` and `$SRV.INFO.<service>.<id>.$openapi`
- Spec is rebuilt eagerly on every `Register()` call so it is always up to date.

### Two-Tier Registration API

**Tier 1 — Typed handler with generics:**

```go
openapi.Register[CreateUserInput, CreateUserOutput](
    api, "create-user", func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
        input := req.Body()
        return &CreateUserOutput{ID: "123"}, nil
    },
    openapi.WithSummary("Create a user"),
)
```

`TypedRequest[I]` embeds `micro.Request` so headers, subject, and raw data remain accessible. The framework deserializes and validates the request body into `I` automatically.

**Tier 2 — Raw handler with schema annotation (no generics):**

```go
api.AddEndpoint("health", micro.HandlerFunc(func(req micro.Request) {
    req.Respond([]byte(`{"status":"ok"}`))
}),
    openapi.WithResponseSchema(`{"type":"object","properties":{"status":{"type":"string"}}}`),
    openapi.WithSummary("Health check"),
)
```

Both tiers go through the `API` wrapper, which is the single source of truth for the OpenAPI spec.

### Schema Generation

- **Library:** `google/jsonschema-go` for both schema inference from Go types and request validation.
- **Tier 1** generates schemas automatically via reflection on `I` and `O` type parameters.
- **Tier 2** accepts raw JSON schema strings for cases where schemas are defined in files or for non-Go types.
- **Validation:** Always validate incoming requests against the generated schema before the handler runs. Invalid requests are rejected with a pre-defined `micro.Error`.

### Handler Signature

```go
type TypedRequest[I any] interface {
    micro.Request
    Body() I
}

type TypedHandler[I, O any] func(TypedRequest[I]) (*O, error)
```

- Body only — no header-to-struct or subject-token-to-struct mapping.
- Headers accessed via `TypedRequest.Headers()` (inherited from `micro.Request`).

### Serialization

- Default: JSON (`encoding/json`).
- Override per-endpoint via `WithCodec(c Codec)` for binary protocols (Protobuf, MsgPack, etc.).

### OpenAPI Metadata per Endpoint

Scoped to the essentials for v1:

- OperationID
- Summary
- Description
- Tags
- Subject token parameters (optional, via `WithSubjectParams("id")`)
- Request and response schemas

Security, deprecation, and other fields deferred to future iterations.

### Subject Parameters

Subjects must be valid NATS subjects (no invented syntax like `{id}`). Wildcard tokens can be optionally named via `WithSubjectParams("id")` which maps positionally to `*` tokens in the subject.

### Groups

- `api.AddGroup("users")` returns a typed `APIGroup` with its own `Register[I,O]` method.
- Groups map to OpenAPI **tags** for documentation grouping. They do not affect the path structure in the spec.

### Error Handling

Use `micro.Error` directly. The framework checks if a returned error is `*micro.Error` and passes it through. Unrecognized errors become a default internal error. Pre-defined sentinel errors:

- `ErrBadRequest` (code 400)
- `ErrValidation` (code 422)
- `ErrInternal` (code 500)

### OpenAPI Document Types

Own minimal structs for the OpenAPI 3.1 fields we need. No dependency on `kin-openapi` or `libopenapi`. We only produce documents, not parse arbitrary ones.

### OpenAPI Version

3.1.x — aligns with JSON Schema draft 2020-12 output from `google/jsonschema-go`.

### Full API Surface

```go
package openapi

// Core wrapper
type API struct{}
type APIGroup struct{}
type APIConfig struct {
    Title          string
    Version        string
    Description    string
    OpenAPISubject string // default: $SRV.INFO.<svc>.$openapi
}

func NewAPI(svc micro.Service, cfg APIConfig) *API
func (a *API) AddGroup(name string, opts ...GroupOpt) *APIGroup
func (a *API) AddEndpoint(name string, handler micro.Handler, opts ...EndpointOpt) error
func (a *API) Spec() []byte

func (g *APIGroup) AddEndpoint(name string, handler micro.Handler, opts ...EndpointOpt) error

// Typed registration
type TypedRequest[I any] interface {
    micro.Request
    Body() I
}
type TypedHandler[I, O any] func(TypedRequest[I]) (*O, error)

func Register[I, O any](target any, name string, handler TypedHandler[I, O], opts ...EndpointOpt) error

// Options
func WithRequestSchema(json string) EndpointOpt
func WithResponseSchema(json string) EndpointOpt
func WithOperationID(id string) EndpointOpt
func WithSummary(s string) EndpointOpt
func WithDescription(d string) EndpointOpt
func WithTags(tags ...string) EndpointOpt
func WithSubjectParams(names ...string) EndpointOpt
func WithSubject(subject string) EndpointOpt
func WithQueueGroup(qg string) EndpointOpt
func WithCodec(c Codec) EndpointOpt

// Codec
type Codec interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
    ContentType() string
}
```

## Decision

| Decision | Choice |
|---|---|
| Primary consumers | HTTP gateway + service discovery/docs |
| Spec location | Metadata fragments + `$SRV.INFO.<svc>.$openapi` endpoint |
| Schema definition | Go type reflection (Tier 1) + raw JSON strings (Tier 2) |
| Handler signature | `TypedRequest[I]` wrapping `micro.Request` with `Body() I` |
| Schema generation library | `google/jsonschema-go` (inference + validation) |
| Serialization | JSON default, `WithCodec()` override |
| OpenAPI version | 3.1.x |
| OpenAPI types | Own minimal structs (no external OpenAPI library) |
| Spec assembly | Eager — rebuilt on every `Register()` call |
| Validation | Always validate incoming requests against schema |
| Error handling | Use `micro.Error` directly with pre-defined codes |
| Subject params | Optional `WithSubjectParams("id")` — valid NATS subjects only |
| Groups | Typed `APIGroup` with `Register[I,O]`, mapped to OpenAPI tags |
| Module location | `github.com/synadia-io/orbit.go/microext/openapi` |
| Tier 2 registration | Through `api.AddEndpoint()` — wrapper is single source of truth |
| Headers/tokens | Body only — headers via `TypedRequest.Headers()` |

## Consequences

- Adds `google/jsonschema-go` as a new dependency to the `microext` module.
- Always-on validation adds latency to every request. High-throughput services that validate at the gateway pay this cost redundantly.
- The `$SRV.INFO.<service>.$openapi` control subject extends the micro protocol's subject namespace without an upstream change. If micro adopts a native OpenAPI subject, this may need migration.
- Own OpenAPI types cover only the subset needed for v1. Users needing full OpenAPI features (security schemes, callbacks, webhooks) will need to wait for future iterations or extend the types themselves.
- Tier 1 typed handlers are a different programming model from raw `micro.Request` handlers. Users mixing both styles in one service need to understand both patterns.
