package response

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/services/contexthandler/ctxkey"
	"github.com/grafana/grafana/pkg/web"
)

type (
	handlerStd       = func(http.ResponseWriter, *http.Request)
	handlerStdCtx    = func(http.ResponseWriter, *http.Request, *web.Context)
	handlerStdReqCtx = func(http.ResponseWriter, *http.Request, *models.ReqContext)
	handlerReqCtx    = func(*models.ReqContext)
	handlerReqCtxRes = func(*models.ReqContext) Response
	handlerCtx       = func(*web.Context)
)

func Wrap(h web.Handler) http.HandlerFunc {
	switch handle := h.(type) {
	case handlerStd:
		return handle
	case handlerStdCtx:
		return func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, web.FromContext(r.Context()))
		}
	case handlerStdReqCtx:
		return func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, FromContext(r.Context()))
		}
	case handlerReqCtx:
		return func(w http.ResponseWriter, r *http.Request) {
			handle(FromContext(r.Context()))
		}
	case handlerReqCtxRes:
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := FromContext(r.Context())
			res := handle(ctx)
			res.WriteTo(ctx)
		}
	case handlerCtx:
		return func(w http.ResponseWriter, r *http.Request) {
			handle(web.FromContext(r.Context()))
		}
	}

	panic(fmt.Sprintf("unexpected handler type: %T", h))
}

func FromContext(ctx context.Context) *models.ReqContext {
	reqCtx, ok := ctx.Value(ctxkey.Key{}).(*models.ReqContext)
	if !ok {
		panic("no *models.ReqContext found")
	}
	return reqCtx
}

func HandlerType(h web.Handler) string {
	switch h.(type) {
	case handlerStd:
		return "HandlerStd"
	case handlerStdCtx:
		return "HandlerStdCtx"
	case handlerReqCtx:
		return "HandlerReqCtx"
	case handlerReqCtxRes:
		return "HandlerReqCtxRes"
	case handlerCtx:
		return "HandlerCtx"
	}

	return "Unknown"
}

const EnvHandlerSummary = "HANDLER_SUMMARY"

func Summary(method, route string, handlers []web.Handler) string {
	out := new(strings.Builder)
	fmt.Fprintf(out, "%s %s:\n", method, route)
	for _, h := range handlers {
		fmt.Fprintf(out, "\t%s\n", HandlerType(h))
	}
	return out.String()
}
