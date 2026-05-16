package openapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go/micro"
)

type Request interface {
	micro.Request

	Context() context.Context

	// Param returns the route param captured by a "*" segment.
	Param(name string) string

	// Wildcard returns the wildcard suffix captured by the ">" segment.
	Wildcard() string
}

type Handler interface {
	HandleRequest(Request)
}

type HandlerFunc func(Request)

func (f HandlerFunc) HandleRequest(r Request) { f(r) }

type Middleware func(Handler) Handler

type request struct {
	micro.Request
	ctx    context.Context
	params map[string]string
}

func (r *request) Context() context.Context { return r.ctx }

func newRequest(req micro.Request) *request {
	return &request{Request: req, ctx: context.Background()}
}

func extractParams(fullPattern string, actualSubject string) map[string]string {
	patternTokens := strings.Split(fullPattern, ".")
	actualTokens := strings.Split(actualSubject, ".")

	params := make(map[string]string)
	paramIdx := 0

	for i, tok := range patternTokens {
		switch {
		case strings.HasPrefix(tok, "{") && strings.HasSuffix(tok, "}"):
			if i >= len(actualTokens) {
				return nil
			}
			name := tok[1 : len(tok)-1]
			params[name] = actualTokens[i]
			paramIdx++
		case tok == "*":
			if i >= len(actualTokens) {
				return nil
			}
			params[fmt.Sprintf("param%d", paramIdx+1)] = actualTokens[i]
			paramIdx++
		case tok == ">":
			if i >= len(actualTokens) {
				return nil
			}
			params["*"] = strings.Join(actualTokens[i:], ".")
			return params
		}
	}

	if len(params) == 0 {
		return nil
	}
	return params
}

func WithContext(req Request, ctx context.Context) Request {
	return &request{Request: req, ctx: ctx}
}

func (r *request) Param(name string) string {
	if r.params == nil {
		return ""
	}
	return r.params[name]
}

func (r *request) Wildcard() string {
	if r.params == nil {
		return ""
	}
	return r.params["*"]
}
