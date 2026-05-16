package test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/synadia-io/orbit.go/microext/openapi"
)

type CreateUserInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUserOutput struct {
	ID string `json:"id"`
}

type GetUserInput struct {
	ID string `json:"id"`
}

type GetUserOutput struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func TestRegisterTypedHandler(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "users",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			input := req.Body()
			return &CreateUserOutput{ID: "user-" + input.Name}, nil
		},
		openapi.WithSummary("Create a user"),
		openapi.WithOperationID("createUser"),
		openapi.WithSubject("users.create"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Make a request through NATS
	msg, err := nc.Request("users.create", []byte(`{"name":"alice","email":"alice@test.com"}`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var output CreateUserOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "user-alice" {
		t.Fatalf("Expected ID 'user-alice', got %q", output.ID)
	}
}

func TestRegisterWithGroup(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "myapp",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	users := api.AddGroup("users")
	err = openapi.Register(
		users, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "123"}, nil
		},
		openapi.WithSummary("Create a user"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("users.create", []byte(`{"name":"bob","email":"bob@test.com"}`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var output CreateUserOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "123" {
		t.Fatalf("Expected ID '123', got %q", output.ID)
	}

	// Prefix groups should not auto-tag
	spec := api.Spec()
	var doc openapi.Document
	if err := json.Unmarshal(spec, &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}

	path, ok := doc.Paths["/users/create"]
	if !ok {
		t.Fatalf("Expected path /users/create in spec, got paths: %v", keysOf(doc.Paths))
	}
	if path.Post == nil {
		t.Fatal("Expected POST operation")
	}
	if len(path.Post.Tags) != 0 {
		t.Fatalf("Expected no tags on prefix group endpoint, got %v", path.Post.Tags)
	}
}

func TestWithGroupSubjectPrefix(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "prefixapp",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	grp := api.AddGroup("v1", openapi.WithGroupSubjectPrefix("api.v1"))
	err = openapi.Register(
		grp, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "prefix-123"}, nil
		},
		openapi.WithSummary("Create user"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Micro group routes on the group name "v1"
	msg, err := nc.Request("v1.create", []byte(`{"name":"alice","email":"a@b.com"}`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var output CreateUserOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "prefix-123" {
		t.Fatalf("Expected ID 'prefix-123', got %q", output.ID)
	}

	// Spec path should use the subject prefix, not the group name
	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}
	if _, ok := doc.Paths["/api/v1/create"]; !ok {
		t.Fatalf("Expected path /api/v1/create, got paths: %v", keysOf(doc.Paths))
	}
	if _, ok := doc.Paths["/v1/create"]; ok {
		t.Fatal("Did not expect path /v1/create — subject prefix should override group name in spec")
	}
}

func TestWithGroupTags(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "tagapp",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	admin := api.AddGroup("admin", openapi.WithGroupTags("admin"))
	err = openapi.Register(
		admin, "list-users",
		func(req openapi.TypedRequest[struct{}]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "1"}, nil
		},
		openapi.WithSummary("List users"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Group should still prefix the subject
	msg, err := nc.Request("admin.list-users", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var output CreateUserOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "1" {
		t.Fatalf("Expected ID '1', got %q", output.ID)
	}

	// Group should apply explicit tags
	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}
	path, ok := doc.Paths["/admin/list-users"]
	if !ok {
		t.Fatalf("Expected path /admin/list-users, got paths: %v", keysOf(doc.Paths))
	}
	if path.Post == nil {
		t.Fatal("Expected POST operation")
	}
	if len(path.Post.Tags) != 1 || path.Post.Tags[0] != "admin" {
		t.Fatalf("Expected tag ['admin'], got %v", path.Post.Tags)
	}
}

func TestValidationRejectsInvalidInput(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "valtest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			t.Fatal("Handler should not be called for invalid JSON")
			return nil, nil
		},
		openapi.WithSubject("valtest.create"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Send invalid JSON
	msg, err := nc.Request("valtest.create", []byte(`not json`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	errCode := msg.Header.Get("Nats-Service-Error-Code")
	if errCode != "400" {
		t.Fatalf("Expected error code 400, got %q", errCode)
	}
}

func TestHandlerErrorPropagation(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "errtest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	notFound := &openapi.Error{Code: "404", Description: "user not found"}

	err = openapi.Register(
		api, "get",
		func(req openapi.TypedRequest[GetUserInput]) (*GetUserOutput, error) {
			return nil, notFound
		},
		openapi.WithSubject("errtest.get"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("errtest.get", []byte(`{"id":"123"}`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if msg.Header.Get("Nats-Service-Error-Code") != "404" {
		t.Fatalf("Expected error code 404, got %q", msg.Header.Get("Nats-Service-Error-Code"))
	}
	if msg.Header.Get("Nats-Service-Error") != "user not found" {
		t.Fatalf("Expected error description 'user not found', got %q", msg.Header.Get("Nats-Service-Error"))
	}

	// Test generic error becomes 500
	err = openapi.Register(
		api, "fail",
		func(req openapi.TypedRequest[GetUserInput]) (*GetUserOutput, error) {
			return nil, errors.New("something broke")
		},
		openapi.WithSubject("errtest.fail"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err = nc.Request("errtest.fail", []byte(`{"id":"123"}`), time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if msg.Header.Get("Nats-Service-Error-Code") != "500" {
		t.Fatalf("Expected error code 500, got %q", msg.Header.Get("Nats-Service-Error-Code"))
	}
}

func TestAddEndpointTier2(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "tier2",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = api.AddEndpoint("health", openapi.HandlerFunc(func(req openapi.Request) {
		req.Respond([]byte(`{"status":"ok"}`))
	}),
		openapi.WithResponseSchema(`{"type":"object","properties":{"status":{"type":"string"}}}`),
		openapi.WithSummary("Health check"),
	)
	if err != nil {
		t.Fatalf("Failed to add endpoint: %v", err)
	}

	// Verify the endpoint works
	msg, err := nc.Request("health", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(msg.Data) != `{"status":"ok"}` {
		t.Fatalf("Expected health response, got %q", string(msg.Data))
	}

	// Verify the spec contains the endpoint
	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}
	path, ok := doc.Paths["/health"]
	if !ok {
		t.Fatalf("Expected path /health in spec")
	}
	if path.Post == nil || path.Post.Summary != "Health check" {
		t.Fatal("Expected POST operation with summary")
	}
	if path.Post.Responses["200"].Content == nil {
		t.Fatal("Expected response schema content")
	}
}

func TestOpenAPIControlSubject(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "spectest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "1"}, nil
		},
		openapi.WithSubject("spectest.create"),
		openapi.WithSummary("Create user"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Query the service-level OpenAPI subject
	msg, err := nc.Request("$SRV.INFO.spectest.$openapi", nil, time.Second)
	if err != nil {
		t.Fatalf("OpenAPI request failed: %v", err)
	}

	var doc openapi.Document
	if err := json.Unmarshal(msg.Data, &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}

	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("Expected OpenAPI 3.1.0, got %q", doc.OpenAPI)
	}
	if doc.Info.Title != "spectest" {
		t.Fatalf("Expected title 'spectest', got %q", doc.Info.Title)
	}
	if _, ok := doc.Paths["/spectest/create"]; !ok {
		t.Fatalf("Expected path /spectest/create, got %v", keysOf(doc.Paths))
	}

	// Query instance-level subject
	info := svc.Info()
	instanceSubject := "$SRV.INFO.spectest." + info.ID + ".$openapi"
	msg, err = nc.Request(instanceSubject, nil, time.Second)
	if err != nil {
		t.Fatalf("Instance OpenAPI request failed: %v", err)
	}

	var doc2 openapi.Document
	if err := json.Unmarshal(msg.Data, &doc2); err != nil {
		t.Fatalf("Unmarshal instance spec: %v", err)
	}
	if doc2.Info.Title != "spectest" {
		t.Fatalf("Instance spec mismatch")
	}
}

func TestSpecSchemaGeneration(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "schemagen",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "1"}, nil
		},
		openapi.WithSubject("schemagen.create"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}

	path := doc.Paths["/schemagen/create"]
	if path == nil || path.Post == nil {
		t.Fatal("Expected path with POST operation")
	}

	// Check request body schema was generated
	if path.Post.RequestBody == nil {
		t.Fatal("Expected request body")
	}
	ct := path.Post.RequestBody.Content["application/json"]
	if ct == nil {
		t.Fatal("Expected application/json content type")
	}

	var reqSchema map[string]any
	if err := json.Unmarshal(ct.Schema, &reqSchema); err != nil {
		t.Fatalf("Unmarshal request schema: %v", err)
	}
	if reqSchema["type"] != "object" {
		t.Fatalf("Expected type 'object', got %v", reqSchema["type"])
	}
	props, ok := reqSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Expected properties in request schema")
	}
	if _, ok := props["name"]; !ok {
		t.Fatal("Expected 'name' property in request schema")
	}
	if _, ok := props["email"]; !ok {
		t.Fatal("Expected 'email' property in request schema")
	}

	// Check response schema
	resp := path.Post.Responses["200"]
	if resp == nil || resp.Content == nil {
		t.Fatal("Expected 200 response with content")
	}
	respCT := resp.Content["application/json"]
	if respCT == nil {
		t.Fatal("Expected application/json in response")
	}

	var respSchema map[string]any
	if err := json.Unmarshal(respCT.Schema, &respSchema); err != nil {
		t.Fatalf("Unmarshal response schema: %v", err)
	}
	respProps, ok := respSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("Expected properties in response schema")
	}
	if _, ok := respProps["id"]; !ok {
		t.Fatal("Expected 'id' property in response schema")
	}
}

func TestSubjectParams(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "paramtest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "get-user",
		func(req openapi.TypedRequest[struct{}]) (*GetUserOutput, error) {
			return &GetUserOutput{ID: req.Param("id"), Name: "Alice", Email: "a@b.com"}, nil
		},
		openapi.WithSubject("users.{id}.profile"),
		openapi.WithOperationID("getUserProfile"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Verify it works via NATS
	msg, err := nc.Request("users.alice.profile", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var output GetUserOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if output.Name != "Alice" {
		t.Fatalf("Expected name 'Alice', got %q", output.Name)
	}
	if output.ID != "alice" {
		t.Fatalf("Expected id 'alice' from Param, got %q", output.ID)
	}

	// Verify spec has parameterized path
	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}

	path, ok := doc.Paths["/users/{id}/profile"]
	if !ok {
		t.Fatalf("Expected path /users/{id}/profile, got %v", keysOf(doc.Paths))
	}
	if path.Post == nil {
		t.Fatal("Expected POST operation")
	}
	if len(path.Post.Parameters) != 1 {
		t.Fatalf("Expected 1 parameter, got %d", len(path.Post.Parameters))
	}
	param := path.Post.Parameters[0]
	if param.Name != "id" || param.In != "path" || !param.Required {
		t.Fatalf("Expected required path param 'id', got %+v", param)
	}
	if path.Post.NATSSubject != "users.*.profile" {
		t.Fatalf("Expected x-nats-subject 'users.*.profile', got %q", path.Post.NATSSubject)
	}
}

func TestEndpointMetadata(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "metatest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "create",
		func(req openapi.TypedRequest[CreateUserInput]) (*CreateUserOutput, error) {
			return &CreateUserOutput{ID: "1"}, nil
		},
		openapi.WithSubject("metatest.create"),
		openapi.WithSummary("Create something"),
		openapi.WithOperationID("createThing"),
		openapi.WithTags("things", "creation"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	// Check that metadata is populated on the micro endpoint
	info := svc.Info()
	var found bool
	for _, ep := range info.Endpoints {
		if ep.Name == "create" {
			found = true
			if ep.Metadata["openapi:summary"] != "Create something" {
				t.Fatalf("Expected summary in metadata, got %v", ep.Metadata)
			}
			if ep.Metadata["openapi:operation-id"] != "createThing" {
				t.Fatalf("Expected operation-id in metadata, got %v", ep.Metadata)
			}
			if ep.Metadata["openapi:tags"] != "things,creation" {
				t.Fatalf("Expected tags in metadata, got %v", ep.Metadata)
			}
			if ep.Metadata["openapi:request-schema"] == "" {
				t.Fatal("Expected request schema in metadata")
			}
			if ep.Metadata["openapi:response-schema"] == "" {
				t.Fatal("Expected response schema in metadata")
			}
		}
	}
	if !found {
		t.Fatal("Endpoint 'create' not found in service info")
	}
}

func TestNoInputType(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "noinput",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	type PingOutput struct {
		Pong bool `json:"pong"`
	}

	err = openapi.Register(
		api, "ping",
		func(req openapi.TypedRequest[struct{}]) (*PingOutput, error) {
			return &PingOutput{Pong: true}, nil
		},
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("ping", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var output PingOutput
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !output.Pong {
		t.Fatal("Expected pong=true")
	}

	// Spec should have no request body
	var doc openapi.Document
	if err := json.Unmarshal(api.Spec(), &doc); err != nil {
		t.Fatalf("Unmarshal spec: %v", err)
	}
	path := doc.Paths["/ping"]
	if path == nil || path.Post == nil {
		t.Fatal("Expected path with POST")
	}
	if path.Post.RequestBody != nil {
		t.Fatal("Expected no request body for struct{} input")
	}
}

func TestContextPropagation(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "ctxtest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	type contextKey string

	traceIDKey := contextKey("trace-id")

	api.Use(func(next openapi.Handler) openapi.Handler {
		return openapi.HandlerFunc(func(req openapi.Request) {
			traceID := req.Headers().Get("X-Trace-ID")
			ctx := context.WithValue(req.Context(), traceIDKey, traceID)
			next.HandleRequest(openapi.WithContext(req, ctx))
		})
	})

	err = openapi.Register(
		api, "echo",
		func(req openapi.TypedRequest[struct{}]) (*CreateUserOutput, error) {
			traceID, _ := req.Context().Value(traceIDKey).(string)
			return &CreateUserOutput{ID: traceID}, nil
		},
		openapi.WithSubject("ctxtest.echo"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg := nats.NewMsg("ctxtest.echo")
	msg.Header.Set("X-Trace-ID", "abc-123")
	resp, err := nc.RequestMsg(msg, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var output CreateUserOutput
	if err := json.Unmarshal(resp.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "abc-123" {
		t.Fatalf("Expected trace ID 'abc-123', got %q", output.ID)
	}
}

func TestContextDefaultsToBackground(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "ctxdefault",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "check",
		func(req openapi.TypedRequest[struct{}]) (*CreateUserOutput, error) {
			if req.Context() == nil {
				return &CreateUserOutput{ID: "nil"}, nil
			}
			return &CreateUserOutput{ID: "ok"}, nil
		},
		openapi.WithSubject("ctxdefault.check"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	resp, err := nc.Request("ctxdefault.check", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	var output CreateUserOutput
	if err := json.Unmarshal(resp.Data, &output); err != nil {
		t.Fatalf("Unmarshal response: %v", err)
	}
	if output.ID != "ok" {
		t.Fatalf("Expected context to be non-nil, got %q", output.ID)
	}
}

func TestAddEndpointWithContext(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "addepctx",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = api.AddEndpoint("ctxcheck", openapi.HandlerFunc(func(req openapi.Request) {
		if req.Context() != nil {
			req.Respond([]byte("has-context"))
		} else {
			req.Respond([]byte("no-context"))
		}
	}))
	if err != nil {
		t.Fatalf("Failed to add endpoint: %v", err)
	}

	resp, err := nc.Request("ctxcheck", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(resp.Data) != "has-context" {
		t.Fatalf("Expected 'has-context', got %q", string(resp.Data))
	}
}

func TestParamExtraction(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "paramext",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "get-user",
		func(req openapi.TypedRequest[struct{}]) (*GetUserOutput, error) {
			return &GetUserOutput{
				ID:    req.Param("id"),
				Name:  req.Param("id"),
				Email: req.Param("missing"),
			}, nil
		},
		openapi.WithSubject("users.{id}.profile"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("users.bob.profile", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var out GetUserOutput
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != "bob" {
		t.Fatalf("Expected Param('id') = 'bob', got %q", out.ID)
	}
	if out.Email != "" {
		t.Fatalf("Expected Param('missing') = '', got %q", out.Email)
	}
}

func TestParamExtractionWithGroup(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "paramgrp",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	grp := api.AddGroup("v1")
	err = openapi.Register(
		grp, "get-user",
		func(req openapi.TypedRequest[struct{}]) (*GetUserOutput, error) {
			return &GetUserOutput{
				ID:   req.Param("id"),
				Name: req.Param("id"),
			}, nil
		},
		openapi.WithSubject("users.{id}.profile"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("v1.users.carol.profile", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var out GetUserOutput
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.ID != "carol" {
		t.Fatalf("Expected Param('id') = 'carol', got %q", out.ID)
	}
}

func TestParamExtractionMultipleParams(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "multiparam",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = openapi.Register(
		api, "get-member",
		func(req openapi.TypedRequest[struct{}]) (*GetUserOutput, error) {
			return &GetUserOutput{
				ID:   req.Param("user"),
				Name: req.Param("org"),
			}, nil
		},
		openapi.WithSubject("orgs.{org}.members.{user}"),
	)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}

	msg, err := nc.Request("orgs.acme.members.alice", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	var out GetUserOutput
	if err := json.Unmarshal(msg.Data, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Name != "acme" {
		t.Fatalf("Expected Param('org') = 'acme', got %q", out.Name)
	}
	if out.ID != "alice" {
		t.Fatalf("Expected Param('user') = 'alice', got %q", out.ID)
	}
}

func TestWildcardExtraction(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "wildcardtest",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = api.AddEndpoint("get-file", openapi.HandlerFunc(func(req openapi.Request) {
		req.Respond([]byte(req.Wildcard()))
	}), openapi.WithSubject("files.>"))
	if err != nil {
		t.Fatalf("Failed to add endpoint: %v", err)
	}

	msg, err := nc.Request("files.docs.readme.txt", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(msg.Data) != "docs.readme.txt" {
		t.Fatalf("Expected Wildcard() = 'docs.readme.txt', got %q", string(msg.Data))
	}
}

func TestNoParamsReturnsEmpty(t *testing.T) {
	s := runServer(t)
	defer s.Shutdown()
	nc := connect(t, s)
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{
		Name:    "noparam",
		Version: "1.0.0",
	})
	if err != nil {
		t.Fatalf("Failed to add service: %v", err)
	}
	defer svc.Stop()

	api, err := openapi.NewAPI(nc, svc, openapi.APIConfig{})
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	err = api.AddEndpoint("health", openapi.HandlerFunc(func(req openapi.Request) {
		result := req.Param("anything") + "|" + req.Wildcard()
		req.Respond([]byte(result))
	}))
	if err != nil {
		t.Fatalf("Failed to add endpoint: %v", err)
	}

	msg, err := nc.Request("health", nil, time.Second)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if string(msg.Data) != "|" {
		t.Fatalf("Expected '|' (empty params), got %q", string(msg.Data))
	}
}

////////////////////////////////////////////////////////////////////////////////

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func runServer(t *testing.T) *server.Server {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	return natsserver.RunServer(&opts)
}

func connect(t *testing.T, s *server.Server) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(s.ClientURL(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	return nc
}
