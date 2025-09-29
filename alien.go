package alien

import "path"
import "sync"
import "errors"
import "strings"
import (
    `net/http`
    `fmt`
)

var (
    eof              = rune(0)
    errRouteNotFound = errors.New("Route not found")
    errBadPattern    = errors.New("bad pattern")
    errUnknownMethod = errors.New("unknown http method")
    headerName       = "_alien"
    AllMethod        = []string{
        http.MethodGet,
        http.MethodPut,
        http.MethodPost,
        http.MethodHead,
        http.MethodPatch,
        http.MethodTrace,
        http.MethodDelete,
        http.MethodOptions,
        http.MethodConnect,
    }
)

type Classify int

const (
    NodeRoot Classify = iota
    nodeParam
    nodeNormal
    nodeCatchAll
    nodeEnd
)

type Node struct {
    key      rune
    value    *Route
    mutex    sync.RWMutex
    classify Classify
    children []*Node
}

func (node *Node) branch(key rune, value *Route, classify ...Classify) *Node {
    data := &Node{
        key:   key,
        value: value,
    }
    if len(classify) > 0 {
        data.classify = classify[0]
    }
    node.children = append(node.children, data)
    return data
}

func (node *Node) findChild(key rune) *Node {
    for _, value := range node.children {
        if value.key == key {
            return value
        }
    }
    return nil
}

func (node *Node) insert(pattern string, value *Route) error {
    node.mutex.Lock()
    defer node.mutex.Unlock()
    if node.classify != NodeRoot {
        return fmt.Errorf("insert on non root node")
    }
    if pattern == "" {
        return errors.New("empty pattern is not support")
    }
    // 47 -> /
    if pattern[0] != 47 {
        return errors.New("path must start with '/'")
    }
    var level *Node
    var child *Node

    for index, character := range pattern {
        if index == 0 {
            level = node
        }
        child = level.findChild(character)
        switch level.classify {
        case nodeParam:
            if index < len(pattern) && character != '/' {
                continue
            }
        }
        if child != nil {
            level = child
            continue
        }
        switch character {
        case ':':
            level = level.branch(character, nil, nodeParam)
        case '*':
            level = level.branch(character, nil, nodeCatchAll)
        default:
            level = level.branch(character, nil, nodeNormal)
        }
    }
    level.branch(eof, value, nodeEnd)
    return nil
}

func (node *Node) find(path string) (*Route, error) {
    node.mutex.RLock()
    defer node.mutex.RUnlock()
    if node.classify != NodeRoot {
        return nil, errors.New("non Node search")
    }
    var level *Node
    var isParam bool
    for k, ch := range path {
        if k == 0 {
            level = node
        }
        c := level.findChild(ch)
        if isParam {
            if k < len(path) && ch != '/' {
                continue
            }
            isParam = false
        }
        param := level.findChild(':')
        if param != nil {
            level = param
            isParam = true
            continue
        }
        catchAll := level.findChild('*')
        if catchAll != nil {
            level = catchAll
            break
        }
        if c != nil {
            level = c
            continue
        }
        return nil, errRouteNotFound
    }
    if level != nil {
        end := level.findChild(eof)
        if end != nil {
            return end.value, nil
        }
        if slash := level.findChild('/'); slash != nil {
            end = slash.findChild(eof)
            if end != nil {
                return end.value, nil
            }
        }
    }
    return nil, errRouteNotFound
}

type Middleware = func(http.Handler) http.Handler

type RouteHandler = func(http.ResponseWriter, *http.Request)

type Route struct {
    path       string
    handler    RouteHandler
    middleware []Middleware
}

func (route *Route) ServeHTTP(response http.ResponseWriter, request *http.Request) {
    var base http.Handler = http.HandlerFunc(route.handler)
    for _, middleware := range route.middleware {
        base = middleware(base)
    }
    base.ServeHTTP(response, request)
}

// ParseParameter parses params found in mateched from pattern.
// There are two kinds of params, one to capture a segment which starts with : and a nother to capture everything( a.k.a catch all) whis starts with *.
//
// For instance
//   pattern:="/hello/:name"
//   matched:="/hello/world"
// Will result into name:world.
// this function captures the named params and theri coreesponding values, returning them in a comma separated  string of a key:value nature.
// please see the tests for more details.
func ParseParameter(match, pattern string) (result string, err error) {
    if strings.Contains(pattern, ":") || strings.Contains(pattern, "*") {
        p1 := strings.Split(match, "/")
        p2 := strings.Split(pattern, "/")
        s1 := len(p1)
        s2 := len(p2)
        if s1 < s2 {
            err = errBadPattern
            return
        }
        for k, v := range p2 {
            if len(v) > 0 {
                switch v[0] {
                case ':':
                    if len(result) == 0 {
                        result = v[1:] + ":" + p1[k]
                        continue
                    }
                    result = result + "," + v[1:] + ":" + p1[k]
                case '*':
                    name := "catch"
                    if k != s2-1 {
                        err = errBadPattern
                        return
                    }
                    if len(v) > 1 {
                        name = v[1:]
                    }
                    if len(result) == 0 {
                        result = name + ":" + strings.Join(p1[k:], "/")
                        return
                    }
                    result = result + "," + name + ":" + strings.Join(p1[k:], "/")
                    return
                }
            }
        }
    }
    return
}

// Parameter 存储路由参数
type Parameter map[string]string

// Load load parameter.
func (parameter Parameter) Load(source string) {
    list := strings.Split(source, ",")
    var variable []string
    for _, value := range list {
        variable = strings.Split(value, ":")
        if len(variable) != 2 {
            continue
        }
        parameter[variable[0]] = variable[1]
    }
}

func (parameter Parameter) Get(key string) string {
    return parameter[key]
}

// GetParameter 返回存储在请求中的路由参数
func GetParameter(request *http.Request) Parameter {
    value := request.Header.Get(headerName)
    if value != "" {
        parameter := make(Parameter)
        parameter.Load(value)
        return parameter
    }
    return nil
}

type Router struct {
    put     *Node
    get     *Node
    post    *Node
    head    *Node
    patch   *Node
    trace   *Node
    delete  *Node
    connect *Node
    options *Node
}

func (r *Router) addRoute(method, path string, h func(http.ResponseWriter, *http.Request), wares ...func(http.Handler) http.Handler) error {
    newRoute := &Route{path: path, handler: h}
    if len(wares) > 0 {
        newRoute.middleware = append(newRoute.middleware, wares...)
    }
    switch method {
    case "GET":
        if r.get == nil {
            r.get = &Node{classify: NodeRoot}
        }
        return r.get.insert(path, newRoute)
    case "POST":
        if r.post == nil {
            r.post = &Node{classify: NodeRoot}
        }
        return r.post.insert(path, newRoute)
    case "PUT":
        if r.put == nil {
            r.put = &Node{classify: NodeRoot}
        }
        return r.put.insert(path, newRoute)
    case "PATCH":
        if r.patch == nil {
            r.patch = &Node{classify: NodeRoot}
        }
        return r.patch.insert(path, newRoute)
    case "HEAD":
        if r.head == nil {
            r.head = &Node{classify: NodeRoot}
        }
        return r.head.insert(path, newRoute)
    case "CONNECT":
        if r.connect == nil {
            r.connect = &Node{classify: NodeRoot}
        }
        return r.connect.insert(path, newRoute)
    case "OPTIONS":
        if r.options == nil {
            r.options = &Node{classify: NodeRoot}
        }
        return r.options.insert(path, newRoute)
    case "TRACE":
        if r.trace == nil {
            r.trace = &Node{classify: NodeRoot}
        }
        return r.trace.insert(path, newRoute)
    case "DELETE":
        if r.delete == nil {
            r.delete = &Node{classify: NodeRoot}
        }
        return r.delete.insert(path, newRoute)
    }
    return errUnknownMethod
}

func (r *Router) find(method, path string) (*Route, error) {
    switch method {
    case "GET":
        if r.get != nil {
            return r.get.find(path)
        }
    case "POST":
        if r.post != nil {
            return r.post.find(path)
        }
    case "PUT":
        if r.put != nil {
            return r.put.find(path)
        }
    case "PATCH":
        if r.patch != nil {
            return r.patch.find(path)
        }
    case "HEAD":
        if r.head != nil {
            return r.head.find(path)
        }
    case "CONNECT":
        if r.connect != nil {
            return r.connect.find(path)
        }
    case "OPTIONS":
        if r.options != nil {
            return r.options.find(path)
        }
    case "TRACE":
        if r.trace != nil {
            return r.trace.find(path)
        }
    case "DELETE":
        if r.delete != nil {
            return r.delete.find(path)
        }
    }
    return nil, errRouteNotFound
}

// Mux is a http multiplexer that allows matching of http requests to the
// registered http handlers.
//
// Mux supports named parameters in urls like
//   /hello/:name
// will match
//   /hello/world
// where by inside the request passed to the handler, the param with key name and
// value world will be passed.
//
// Mux supports catch all parameters too
//   /hello/*whatever
// will match
//   /hello/world
//   /hello/world/tanzania
//   /hello/world/afica/tanzania.png
// where by inside the request passed to the handler, the param with key
// whatever will be set and value will be
//   world
//   world/tanzania
//   world/afica/tanzania.png
//
// If you dont specify a name in a catch all Route, then the default name "catch" will be used.
type Mux struct {
    *Router
    prefix     string
    notFound   http.Handler
    middleware []func(http.Handler) http.Handler
}

// New returns a new *Mux instance with default handler for mismatched routes.
func New() *Mux {
    m := &Mux{}
    m.Router = &Router{}
    m.notFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, errRouteNotFound.Error(), http.StatusNotFound)
    })
    return m
}

// AddRoute registers h with pattern and method. If there is a path prefix
// created via the Group method) it will be set.
func (mux *Mux) AddRoute(method, pattern string, h func(http.ResponseWriter, *http.Request)) error {
    if mux.prefix != "" {
        pattern = path.Join(mux.prefix, pattern)
    }
    return mux.addRoute(method, pattern, h, mux.middleware...)
}

// Get registers h with pattern and method GET.
func (mux *Mux) Get(pattern string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("GET", pattern, h)
}

// Put registers h with pattern and method PUT.
func (mux *Mux) Put(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("PUT", path, h)
}

// Post registers h with pattern and method POST.
func (mux *Mux) Post(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("POST", path, h)
}

// Patch registers h with pattern and method PATCH.
func (mux *Mux) Patch(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("PATCH", path, h)
}

// Head registers h with pattern and method HEAD.
func (mux *Mux) Head(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("HEAD", path, h)
}

// Options registers h with pattern and method OPTIONS.
func (mux *Mux) Options(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("OPTIONS", path, h)
}

// Connect  registers h with pattern and method CONNECT.
func (mux *Mux) Connect(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("CONNECT", path, h)
}

// Trace registers h with pattern and method TRACE.
func (mux *Mux) Trace(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("TRACE", path, h)
}

// Delete registers h with pattern and method DELETE.
func (mux *Mux) Delete(path string, h func(http.ResponseWriter, *http.Request)) error {
    return mux.AddRoute("DELETE", path, h)
}

// ContainsRoute checks if a Route is present in the Mux.
// It takes two arguments: path (string) and method (string).
// If method is empty, it will look for the Route in all HTTP methods.
// It returns a boolean value indicating whether the Route is found or not, along with an error if any.
func (mux *Mux) ContainsRoute(path, method string) (bool, error) {
    findRoute := func(method string) (bool, error) {
        _, err := mux.find(method, path)
        if err == nil {
            return true, nil
        }
        if !errors.Is(err, errRouteNotFound) {
            return false, err
        }
        return false, nil
    }
    if method != "" {
        return findRoute(method)
    }
    for _, method = range AllMethod {
        ok, err := findRoute(method)
        if ok || err != nil {
            return ok, err
        }
    }
    return false, nil
}

func (mux *Mux) NotFoundHandler(handler http.Handler) {
    mux.notFound = handler
}

// ServeHTTP implements http.Handler interface
func (mux *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    p := path.Clean(r.URL.Path)
    h, err := mux.find(r.Method, p)
    if err != nil {
        mux.notFound.ServeHTTP(w, r)
        return
    }
    params, _ := ParseParameter(p, h.path) // check if there is any url params
    if params != "" {
        r.Header.Set(headerName, params)
    }
    h.ServeHTTP(w, r)
}

// Group creates a path prefix group for pattern, all routes registered using
// the returned Mux will only match if the request path starts with pattern. For
// instance .
//   m:=New()
//   home:=m.Group("/home")
//   home.Get("/alone",myHandler)
// will match
//   /home/alone
func (mux *Mux) Group(pattern string) *Mux {
    return &Mux{
        prefix:     pattern,
        Router:     mux.Router,
        middleware: mux.middleware,
        notFound:   mux.notFound,
    }

}

// Use assigns midlewares to the current *Mux. All routes registered by the *Mux
// after this call will have the middlewares assigned to them.
func (mux *Mux) Use(middleware ...func(http.Handler) http.Handler) {
    if len(middleware) > 0 {
        mux.middleware = append(mux.middleware, middleware...)
    }
}
