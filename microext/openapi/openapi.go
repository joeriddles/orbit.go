package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
)

const (
	MetaKeyRequestSchema  = "openapi:request-schema"
	MetaKeyResponseSchema = "openapi:response-schema"
	MetaKeyOperationID    = "openapi:operation-id"
	MetaKeySummary        = "openapi:summary"
	MetaKeyDescription    = "openapi:description"
	MetaKeyTags           = "openapi:tags"
	MetaKeySubjectParams  = "openapi:subject-params"
)

type APIConfig struct {
	OpenAPISubject string
}

type API struct {
	nc       *nats.Conn
	svc      micro.Service
	cfg      APIConfig
	ops      []*operationInfo
	specJSON []byte
	subs     []*nats.Subscription
	mu       sync.Mutex
}

type APIGroup struct {
	api    *API
	group  micro.Group
	prefix string
}

type operationInfo struct {
	name           string
	subject        string
	operationID    string
	summary        string
	description    string
	tags           []string
	subjectParams  []string
	requestSchema  json.RawMessage
	responseSchema json.RawMessage
	contentType    string
}

type TypedHandler[I, O any] func(TypedRequest[I]) (*O, error)

type TypedRequest[I any] interface {
	micro.Request
	Body() I
}

type typedRequest[I any] struct {
	micro.Request
	body I
}

func (r *typedRequest[I]) Body() I { return r.body }

func NewAPI(nc *nats.Conn, svc micro.Service, cfg APIConfig) (*API, error) {
	api := &API{
		nc:  nc,
		svc: svc,
		cfg: cfg,
	}

	info := svc.Info()

	subject := cfg.OpenAPISubject
	if subject == "" {
		subject = fmt.Sprintf("$SRV.INFO.%s.$openapi", info.Name)
	}

	handler := func(m *nats.Msg) {
		api.mu.Lock()
		spec := api.specJSON
		api.mu.Unlock()
		m.Respond(spec)
	}

	sub1, err := nc.Subscribe(subject, handler)
	if err != nil {
		return nil, fmt.Errorf("subscribing to %s: %w", subject, err)
	}
	api.subs = append(api.subs, sub1)

	instanceSubject := fmt.Sprintf("$SRV.INFO.%s.%s.$openapi", info.Name, info.ID)
	sub2, err := nc.Subscribe(instanceSubject, handler)
	if err != nil {
		sub1.Unsubscribe()
		return nil, fmt.Errorf("subscribing to %s: %w", instanceSubject, err)
	}
	api.subs = append(api.subs, sub2)

	api.rebuildSpec()
	return api, nil
}

func (a *API) AddGroup(name string, opts ...GroupOpt) *APIGroup {
	g := a.svc.AddGroup(name, opts...)
	return &APIGroup{
		api:    a,
		group:  g,
		prefix: name,
	}
}

func (a *API) AddEndpoint(name string, handler micro.Handler, opts ...EndpointOpt) error {
	var cfg endpointConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.subject == "" {
		cfg.subject = name
	}

	microOpts := buildMicroOpts(&cfg)
	if err := a.svc.AddEndpoint(name, handler, microOpts...); err != nil {
		return err
	}

	op := newOperationInfo(name, &cfg)
	a.mu.Lock()
	a.ops = append(a.ops, op)
	a.rebuildSpecLocked()
	a.mu.Unlock()
	return nil
}

func (g *APIGroup) AddEndpoint(name string, handler micro.Handler, opts ...EndpointOpt) error {
	var cfg endpointConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.subject == "" {
		cfg.subject = name
	}
	cfg.subject = joinSubject(g.prefix, cfg.subject)

	microOpts := buildMicroOpts(&cfg)
	if err := g.group.AddEndpoint(name, handler, microOpts...); err != nil {
		return err
	}

	op := newOperationInfo(name, &cfg)
	if len(op.tags) == 0 {
		op.tags = []string{g.prefix}
	}

	g.api.mu.Lock()
	g.api.ops = append(g.api.ops, op)
	g.api.rebuildSpecLocked()
	g.api.mu.Unlock()
	return nil
}

func (g *APIGroup) AddGroup(name string, opts ...GroupOpt) *APIGroup {
	sub := g.group.AddGroup(name, opts...)
	return &APIGroup{
		api:    g.api,
		group:  sub,
		prefix: joinSubject(g.prefix, name),
	}
}

func (a *API) Spec() []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.specJSON
}

func Register[I, O any](target any, name string, handler TypedHandler[I, O], opts ...EndpointOpt) error {
	var cfg endpointConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	codec := cfg.codec
	if codec == nil {
		codec = defaultCodec
	}

	reqSchemaJSON, reqResolved, err := generateSchema[I]()
	if err != nil {
		return fmt.Errorf("generating request schema: %w", err)
	}

	respSchemaJSON, err := generateSchemaJSON[O]()
	if err != nil {
		return fmt.Errorf("generating response schema: %w", err)
	}

	if cfg.requestSchema != "" {
		reqSchemaJSON = json.RawMessage(cfg.requestSchema)
		var s jsonschema.Schema
		if err := json.Unmarshal(reqSchemaJSON, &s); err != nil {
			return fmt.Errorf("parsing request schema: %w", err)
		}
		reqResolved, err = s.Resolve(nil)
		if err != nil {
			return fmt.Errorf("resolving request schema: %w", err)
		}
	}
	if cfg.responseSchema != "" {
		respSchemaJSON = json.RawMessage(cfg.responseSchema)
	}

	hasInput := reflect.TypeFor[I]() != reflect.TypeFor[struct{}]()

	wrappedHandler := micro.HandlerFunc(func(req micro.Request) {
		var input I

		if hasInput {
			if err := codec.Unmarshal(req.Data(), &input); err != nil {
				req.Error(ErrBadRequest.Code, ErrBadRequest.Description, []byte(err.Error()))
				return
			}

			if reqResolved != nil {
				var instance any
				if err := json.Unmarshal(req.Data(), &instance); err != nil {
					req.Error(ErrBadRequest.Code, ErrBadRequest.Description, []byte(err.Error()))
					return
				}
				if err := reqResolved.Validate(instance); err != nil {
					req.Error(ErrValidation.Code, ErrValidation.Description, []byte(err.Error()))
					return
				}
			}
		}

		output, handlerErr := handler(&typedRequest[I]{Request: req, body: input})
		if handlerErr != nil {
			var svcErr *Error
			if errors.As(handlerErr, &svcErr) {
				req.Error(svcErr.Code, svcErr.Description, nil)
			} else {
				req.Error(ErrInternal.Code, ErrInternal.Description, []byte(handlerErr.Error()))
			}
			return
		}

		if output == nil {
			req.Respond(nil)
			return
		}

		data, marshalErr := codec.Marshal(output)
		if marshalErr != nil {
			req.Error(ErrInternal.Code, ErrInternal.Description, []byte(marshalErr.Error()))
			return
		}
		req.Respond(data)
	})

	op := &operationInfo{
		operationID:    cfg.operationID,
		summary:        cfg.summary,
		description:    cfg.description,
		tags:           cfg.tags,
		subjectParams:  cfg.subjectParams,
		requestSchema:  reqSchemaJSON,
		responseSchema: respSchemaJSON,
		contentType:    codec.ContentType(),
	}

	switch t := target.(type) {
	case *API:
		return t.registerTyped(name, wrappedHandler, &cfg, op)
	case *APIGroup:
		return t.registerTyped(name, wrappedHandler, &cfg, op)
	default:
		return fmt.Errorf("target must be *API or *APIGroup, got %T", target)
	}
}

func (a *API) registerTyped(name string, handler micro.Handler, cfg *endpointConfig, op *operationInfo) error {
	if cfg.subject == "" {
		cfg.subject = name
	}

	microOpts := buildMicroOptsFromOp(cfg, op)
	if err := a.svc.AddEndpoint(name, handler, microOpts...); err != nil {
		return err
	}

	op.name = name
	op.subject = cfg.subject

	a.mu.Lock()
	a.ops = append(a.ops, op)
	a.rebuildSpecLocked()
	a.mu.Unlock()
	return nil
}

func (g *APIGroup) registerTyped(name string, handler micro.Handler, cfg *endpointConfig, op *operationInfo) error {
	subject := name
	if cfg.subject != "" {
		subject = cfg.subject
	}
	fullSubject := joinSubject(g.prefix, subject)

	microOpts := buildMicroOptsFromOp(cfg, op)
	if err := g.group.AddEndpoint(name, handler, microOpts...); err != nil {
		return err
	}

	op.name = name
	op.subject = fullSubject
	if len(op.tags) == 0 {
		op.tags = []string{g.prefix}
	}

	g.api.mu.Lock()
	g.api.ops = append(g.api.ops, op)
	g.api.rebuildSpecLocked()
	g.api.mu.Unlock()
	return nil
}

func (a *API) rebuildSpec() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rebuildSpecLocked()
}

func (a *API) rebuildSpecLocked() {
	microInfo := a.svc.Info()

	doc := &Document{
		OpenAPI: "3.1.0",
		Info: DocumentInfo{
			Title:       microInfo.Name,
			Version:     microInfo.Version,
			Description: microInfo.Description,
		},
		Paths: make(map[string]*PathItem),
	}

	for _, op := range a.ops {
		ct := op.contentType
		if ct == "" {
			ct = "application/json"
		}

		oper := &Operation{
			OperationID: op.operationID,
			Summary:     op.summary,
			Description: op.description,
			Tags:        op.tags,
			NATSSubject: op.subject,
			Parameters:  buildParameters(op.subject, op.subjectParams),
		}

		if len(op.requestSchema) > 0 {
			oper.RequestBody = &RequestBody{
				Required: true,
				Content: map[string]*MediaType{
					ct: {Schema: op.requestSchema},
				},
			}
		}

		responses := map[string]*Response{
			"200": {Description: "Successful response"},
		}
		if len(op.responseSchema) > 0 {
			responses["200"].Content = map[string]*MediaType{
				ct: {Schema: op.responseSchema},
			}
		}
		oper.Responses = responses

		path := subjectToPath(op.subject, op.subjectParams)
		doc.Paths[path] = &PathItem{Post: oper}
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return
	}
	a.specJSON = data
}

func generateSchema[T any]() (json.RawMessage, *jsonschema.Resolved, error) {
	t := reflect.TypeFor[T]()
	if t == reflect.TypeFor[struct{}]() {
		return nil, nil, nil
	}

	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, nil, err
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, nil, err
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, nil, err
	}

	return data, resolved, nil
}

func generateSchemaJSON[T any]() (json.RawMessage, error) {
	t := reflect.TypeFor[T]()
	if t == reflect.TypeFor[struct{}]() {
		return nil, nil
	}

	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func newOperationInfo(name string, cfg *endpointConfig) *operationInfo {
	op := &operationInfo{
		name:          name,
		subject:       cfg.subject,
		operationID:   cfg.operationID,
		summary:       cfg.summary,
		description:   cfg.description,
		tags:          cfg.tags,
		subjectParams: cfg.subjectParams,
		contentType:   "application/json",
	}
	if cfg.requestSchema != "" {
		op.requestSchema = json.RawMessage(cfg.requestSchema)
	}
	if cfg.responseSchema != "" {
		op.responseSchema = json.RawMessage(cfg.responseSchema)
	}
	return op
}

func buildMicroOpts(cfg *endpointConfig) []micro.EndpointOpt {
	var opts []micro.EndpointOpt
	if cfg.subject != "" {
		opts = append(opts, micro.WithEndpointSubject(cfg.subject))
	}
	if cfg.queueGroup != "" {
		opts = append(opts, micro.WithEndpointQueueGroup(cfg.queueGroup))
	}

	meta := buildMetadata(cfg.operationID, cfg.summary, cfg.description,
		cfg.tags, cfg.subjectParams, cfg.requestSchema, cfg.responseSchema)
	if len(meta) > 0 {
		opts = append(opts, micro.WithEndpointMetadata(meta))
	}
	return opts
}

func buildMicroOptsFromOp(cfg *endpointConfig, op *operationInfo) []micro.EndpointOpt {
	var opts []micro.EndpointOpt
	if cfg.subject != "" {
		opts = append(opts, micro.WithEndpointSubject(cfg.subject))
	}
	if cfg.queueGroup != "" {
		opts = append(opts, micro.WithEndpointQueueGroup(cfg.queueGroup))
	}

	meta := buildMetadata(op.operationID, op.summary, op.description,
		op.tags, op.subjectParams, string(op.requestSchema), string(op.responseSchema))
	if len(meta) > 0 {
		opts = append(opts, micro.WithEndpointMetadata(meta))
	}
	return opts
}

func buildMetadata(operationID, summary, description string, tags, subjectParams []string, reqSchema, respSchema string) map[string]string {
	meta := make(map[string]string)
	if operationID != "" {
		meta[MetaKeyOperationID] = operationID
	}
	if summary != "" {
		meta[MetaKeySummary] = summary
	}
	if description != "" {
		meta[MetaKeyDescription] = description
	}
	if len(tags) > 0 {
		meta[MetaKeyTags] = strings.Join(tags, ",")
	}
	if len(subjectParams) > 0 {
		meta[MetaKeySubjectParams] = strings.Join(subjectParams, ",")
	}
	if reqSchema != "" {
		meta[MetaKeyRequestSchema] = reqSchema
	}
	if respSchema != "" {
		meta[MetaKeyResponseSchema] = respSchema
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func joinSubject(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ".")
}
