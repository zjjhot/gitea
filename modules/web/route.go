// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package web

import (
	"net/http"
	"net/url"
	"strings"

	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/web/middleware"

	"gitea.com/go-chi/binding"
	"github.com/go-chi/chi/v5"
)

// Bind binding an obj to a handler's context data
func Bind[T any](_ T) http.HandlerFunc {
	return func(resp http.ResponseWriter, req *http.Request) {
		theObj := new(T) // create a new form obj for every request but not use obj directly
		data := middleware.GetContextData(req.Context())
		binding.Bind(req, theObj)
		SetForm(data, theObj)
		middleware.AssignForm(theObj, data)
	}
}

// SetForm set the form object
func SetForm(dataStore middleware.ContextDataStore, obj any) {
	dataStore.GetData()["__form"] = obj
}

// GetForm returns the validate form information
func GetForm(dataStore middleware.ContextDataStore) any {
	return dataStore.GetData()["__form"]
}

// Router defines a route based on chi's router
type Router struct {
	chiRouter      chi.Router
	curGroupPrefix string
	curMiddlewares []any
}

// NewRouter creates a new route
func NewRouter() *Router {
	r := chi.NewRouter()
	return &Router{chiRouter: r}
}

// Use supports two middlewares
func (r *Router) Use(middlewares ...any) {
	for _, m := range middlewares {
		if m != nil {
			r.chiRouter.Use(toHandlerProvider(m))
		}
	}
}

// Group mounts a sub-Router along a `pattern` string.
func (r *Router) Group(pattern string, fn func(), middlewares ...any) {
	previousGroupPrefix := r.curGroupPrefix
	previousMiddlewares := r.curMiddlewares
	r.curGroupPrefix += pattern
	r.curMiddlewares = append(r.curMiddlewares, middlewares...)

	fn()

	r.curGroupPrefix = previousGroupPrefix
	r.curMiddlewares = previousMiddlewares
}

func (r *Router) getPattern(pattern string) string {
	newPattern := r.curGroupPrefix + pattern
	if !strings.HasPrefix(newPattern, "/") {
		newPattern = "/" + newPattern
	}
	if newPattern == "/" {
		return newPattern
	}
	return strings.TrimSuffix(newPattern, "/")
}

func (r *Router) wrapMiddlewareAndHandler(h []any) ([]func(http.Handler) http.Handler, http.HandlerFunc) {
	handlerProviders := make([]func(http.Handler) http.Handler, 0, len(r.curMiddlewares)+len(h)+1)
	for _, m := range r.curMiddlewares {
		if m != nil {
			handlerProviders = append(handlerProviders, toHandlerProvider(m))
		}
	}
	for _, m := range h {
		if h != nil {
			handlerProviders = append(handlerProviders, toHandlerProvider(m))
		}
	}
	middlewares := handlerProviders[:len(handlerProviders)-1]
	handlerFunc := handlerProviders[len(handlerProviders)-1](nil).ServeHTTP
	mockPoint := RouterMockPoint(MockAfterMiddlewares)
	if mockPoint != nil {
		middlewares = append(middlewares, mockPoint)
	}
	return middlewares, handlerFunc
}

// Methods adds the same handlers for multiple http "methods" (separated by ",").
// If any method is invalid, the lower level router will panic.
func (r *Router) Methods(methods, pattern string, h ...any) {
	middlewares, handlerFunc := r.wrapMiddlewareAndHandler(h)
	fullPattern := r.getPattern(pattern)
	if strings.Contains(methods, ",") {
		methods := strings.Split(methods, ",")
		for _, method := range methods {
			r.chiRouter.With(middlewares...).Method(strings.TrimSpace(method), fullPattern, handlerFunc)
		}
	} else {
		r.chiRouter.With(middlewares...).Method(methods, fullPattern, handlerFunc)
	}
}

// Mount attaches another Router along ./pattern/*
func (r *Router) Mount(pattern string, subRouter *Router) {
	subRouter.Use(r.curMiddlewares...)
	r.chiRouter.Mount(r.getPattern(pattern), subRouter.chiRouter)
}

// Any delegate requests for all methods
func (r *Router) Any(pattern string, h ...any) {
	middlewares, handlerFunc := r.wrapMiddlewareAndHandler(h)
	r.chiRouter.With(middlewares...).HandleFunc(r.getPattern(pattern), handlerFunc)
}

// Delete delegate delete method
func (r *Router) Delete(pattern string, h ...any) {
	r.Methods("DELETE", pattern, h...)
}

// Get delegate get method
func (r *Router) Get(pattern string, h ...any) {
	r.Methods("GET", pattern, h...)
}

// Head delegate head method
func (r *Router) Head(pattern string, h ...any) {
	r.Methods("HEAD", pattern, h...)
}

// Post delegate post method
func (r *Router) Post(pattern string, h ...any) {
	r.Methods("POST", pattern, h...)
}

// Put delegate put method
func (r *Router) Put(pattern string, h ...any) {
	r.Methods("PUT", pattern, h...)
}

// Patch delegate patch method
func (r *Router) Patch(pattern string, h ...any) {
	r.Methods("PATCH", pattern, h...)
}

// ServeHTTP implements http.Handler
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.normalizeRequestPath(w, req, r.chiRouter)
}

// NotFound defines a handler to respond whenever a route could not be found.
func (r *Router) NotFound(h http.HandlerFunc) {
	r.chiRouter.NotFound(h)
}

func (r *Router) normalizeRequestPath(resp http.ResponseWriter, req *http.Request, next http.Handler) {
	normalized := false
	normalizedPath := req.URL.EscapedPath()
	if normalizedPath == "" {
		normalizedPath, normalized = "/", true
	} else if normalizedPath != "/" {
		normalized = strings.HasSuffix(normalizedPath, "/")
		normalizedPath = strings.TrimRight(normalizedPath, "/")
	}
	removeRepeatedSlashes := strings.Contains(normalizedPath, "//")
	normalized = normalized || removeRepeatedSlashes

	// the following code block is a slow-path for replacing all repeated slashes "//" to one single "/"
	// if the path doesn't have repeated slashes, then no need to execute it
	if removeRepeatedSlashes {
		buf := &strings.Builder{}
		for i := 0; i < len(normalizedPath); i++ {
			if i == 0 || normalizedPath[i-1] != '/' || normalizedPath[i] != '/' {
				buf.WriteByte(normalizedPath[i])
			}
		}
		normalizedPath = buf.String()
	}

	// If the config tells Gitea to use a sub-url path directly without reverse proxy,
	// then we need to remove the sub-url path from the request URL path.
	// But "/v2" is special for OCI container registry, it should always be in the root of the site.
	if setting.UseSubURLPath {
		remainingPath, ok := strings.CutPrefix(normalizedPath, setting.AppSubURL+"/")
		if ok {
			normalizedPath = "/" + remainingPath
		} else if normalizedPath == setting.AppSubURL {
			normalizedPath = "/"
		} else if !strings.HasPrefix(normalizedPath+"/", "/v2/") {
			// do not respond to other requests, to simulate a real sub-path environment
			http.Error(resp, "404 page not found, sub-path is: "+setting.AppSubURL, http.StatusNotFound)
			return
		}
		normalized = true
	}

	// if the path is normalized, then fill it back to the request
	if normalized {
		decodedPath, err := url.PathUnescape(normalizedPath)
		if err != nil {
			http.Error(resp, "400 Bad Request: unable to unescape path "+normalizedPath, http.StatusBadRequest)
			return
		}
		req.URL.RawPath = normalizedPath
		req.URL.Path = decodedPath
	}

	next.ServeHTTP(resp, req)
}

// Combo delegates requests to Combo
func (r *Router) Combo(pattern string, h ...any) *Combo {
	return &Combo{r, pattern, h}
}

// Combo represents a tiny group routes with same pattern
type Combo struct {
	r       *Router
	pattern string
	h       []any
}

// Get delegates Get method
func (c *Combo) Get(h ...any) *Combo {
	c.r.Get(c.pattern, append(c.h, h...)...)
	return c
}

// Post delegates Post method
func (c *Combo) Post(h ...any) *Combo {
	c.r.Post(c.pattern, append(c.h, h...)...)
	return c
}

// Delete delegates Delete method
func (c *Combo) Delete(h ...any) *Combo {
	c.r.Delete(c.pattern, append(c.h, h...)...)
	return c
}

// Put delegates Put method
func (c *Combo) Put(h ...any) *Combo {
	c.r.Put(c.pattern, append(c.h, h...)...)
	return c
}

// Patch delegates Patch method
func (c *Combo) Patch(h ...any) *Combo {
	c.r.Patch(c.pattern, append(c.h, h...)...)
	return c
}
