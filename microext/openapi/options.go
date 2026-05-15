package openapi

import "github.com/nats-io/nats.go/micro"

type EndpointOpt func(*endpointConfig)

type GroupOpt = micro.GroupOpt

type endpointConfig struct {
	subject        string
	operationID    string
	summary        string
	description    string
	tags           []string
	subjectParams  []string
	requestSchema  string
	responseSchema string
	queueGroup     string
	codec          Codec
}

func WithSubject(subject string) EndpointOpt {
	return func(c *endpointConfig) { c.subject = subject }
}

func WithOperationID(id string) EndpointOpt {
	return func(c *endpointConfig) { c.operationID = id }
}

func WithSummary(s string) EndpointOpt {
	return func(c *endpointConfig) { c.summary = s }
}

func WithDescription(d string) EndpointOpt {
	return func(c *endpointConfig) { c.description = d }
}

func WithTags(tags ...string) EndpointOpt {
	return func(c *endpointConfig) { c.tags = tags }
}

func WithSubjectParams(names ...string) EndpointOpt {
	return func(c *endpointConfig) { c.subjectParams = names }
}

func WithRequestSchema(schema string) EndpointOpt {
	return func(c *endpointConfig) { c.requestSchema = schema }
}

func WithResponseSchema(schema string) EndpointOpt {
	return func(c *endpointConfig) { c.responseSchema = schema }
}

func WithQueueGroup(qg string) EndpointOpt {
	return func(c *endpointConfig) { c.queueGroup = qg }
}

func WithCodec(c Codec) EndpointOpt {
	return func(cfg *endpointConfig) { cfg.codec = c }
}
