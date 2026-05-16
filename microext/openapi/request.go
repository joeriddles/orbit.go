package openapi

import (
	"context"

	"github.com/nats-io/nats.go/micro"
)

type Request interface {
	micro.Request
	Context() context.Context
}

type Handler interface {
	HandleRequest(Request)
}

type HandlerFunc func(Request)

func (f HandlerFunc) HandleRequest(r Request) { f(r) }

type Middleware func(Handler) Handler

type request struct {
	micro.Request
	ctx context.Context
}

func (r *request) Context() context.Context { return r.ctx }

func newRequest(req micro.Request) *request {
	return &request{Request: req, ctx: context.Background()}
}

func WithContext(req Request, ctx context.Context) Request {
	return &request{Request: req, ctx: ctx}
}
