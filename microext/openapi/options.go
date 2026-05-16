package openapi

import "github.com/nats-io/nats.go/micro"

type EndpointOpt func(*endpointConfig)

type GroupOpt func(*groupConfig)

type groupConfig struct {
	subjectPrefix string
	tags          []string
	microOpts     []micro.GroupOpt
}

func WithGroupSubjectPrefix(prefix string) GroupOpt {
	return func(c *groupConfig) { c.subjectPrefix = prefix }
}

func WithGroupTags(tags ...string) GroupOpt {
	return func(c *groupConfig) { c.tags = tags }
}

func parseGroupOpts(opts []GroupOpt) groupConfig {
	var cfg groupConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

type endpointConfig struct {
	subject        string
	operationID    string
	summary        string
	description    string
	tags           []string
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
