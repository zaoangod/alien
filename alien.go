package alien

import "fmt"
import "path"
import "sync"
import "errors"
import "strings"
import "net/http"

type Lock = sync.RWMutex

var (
    ErrorRouteNotFound = errors.New("route not found")

    errBadPattern = errors.New("bad pattern")
    headerName    = "_alien"
    AllMethod     = []string{
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

const EOF = rune(0)
const (
    NodeRoot = iota
    NodeParameter
    NodeNormal
    NodeCatchAll
    NodeEnd
)

type Node struct {
    key      rune
    value    *Route
    lock     Lock
    classify byte
    children []*Node
}

func (node *Node) branch(key rune, value *Route, classify ...byte) *Node {
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

func (node *Node) child(key rune) *Node {
    for _, value := range node.children {
        if value.key == key {
            return value
        }
    }
    return nil
}

func (node *Node) insert(pattern string, value *Route) error {
    node.lock.Lock()
    defer node.lock.Unlock()
    if node.classify != NodeRoot {
        return fmt.Errorf("insert on non root node")
    }
    if pattern == "" {
        return errors.New("empty pattern is not support")
    }
    if pattern[0] != 47 {
        return errors.New("path must start with '/'")
    }
    var level = node
    for index, character := range pattern {
        var child = level.child(character)
        if level.classify == NodeParameter && index < len(pattern) && character != '/' {
            continue
        }
        if child != nil {
            level = child
            continue
        }
        switch character {
        case ':':
            level = level.branch(character, nil, NodeParameter)
        case '*':
            level = level.branch(character, nil, NodeCatchAll)
        default:
            level = level.branch(character, nil, NodeNormal)
        }
    }
    level.branch(EOF, value, NodeEnd)
    return nil
}

func (node *Node) find(pattern string) (*Route, error) {
    node.lock.RLock()
    defer node.lock.RUnlock()
    if node.classify != NodeRoot {
        return nil, errors.New("non Node search")
    }
    var level *Node
    var isParameter bool
    for index, character := range pattern {
        if index == 0 {
            level = node
        }
        c := level.child(character)
        if isParameter {
            if index < len(pattern) && character != '/' {
                continue
            }
            isParameter = false
        }
        param := level.child(':')
        if param != nil {
            level = param
            isParameter = true
            continue
        }
        catchAll := level.child('*')
        if catchAll != nil {
            level = catchAll
            break
        }
        if c != nil {
            level = c
            continue
        }
        return nil, ErrorRouteNotFound
    }
    if level != nil {
        end := level.child(EOF)
        if end != nil {
            return end.value, nil
        }
        if slash := level.child('/'); slash != nil {
            end = slash.child(EOF)
            if end != nil {
                return end.value, nil
            }
        }
    }
    return nil, ErrorRouteNotFound
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

func (router *Router) addRoute(method, path string, handler RouteHandler, middleware ...Middleware) error {
    value := &Route{
        path:    path,
        handler: handler,
    }
    if len(middleware) > 0 {
        value.middleware = append(value.middleware, middleware...)
    }
    switch method {
    case http.MethodGet:
        if router.get == nil {
            router.get = &Node{classify: NodeRoot}
        }
        return router.get.insert(path, value)
    case http.MethodPut:
        if router.put == nil {
            router.put = &Node{classify: NodeRoot}
        }
        return router.put.insert(path, value)
    case http.MethodPost:
        if router.post == nil {
            router.post = &Node{classify: NodeRoot}
        }
        return router.post.insert(path, value)
    case http.MethodHead:
        if router.head == nil {
            router.head = &Node{classify: NodeRoot}
        }
        return router.head.insert(path, value)
    case http.MethodPatch:
        if router.patch == nil {
            router.patch = &Node{classify: NodeRoot}
        }
        return router.patch.insert(path, value)
    case http.MethodTrace:
        if router.trace == nil {
            router.trace = &Node{classify: NodeRoot}
        }
        return router.trace.insert(path, value)
    case http.MethodDelete:
        if router.delete == nil {
            router.delete = &Node{classify: NodeRoot}
        }
        return router.delete.insert(path, value)
    case http.MethodConnect:
        if router.connect == nil {
            router.connect = &Node{classify: NodeRoot}
        }
        return router.connect.insert(path, value)
    case http.MethodOptions:
        if router.options == nil {
            router.options = &Node{classify: NodeRoot}
        }
        return router.options.insert(path, value)
    }
    return errors.New("unknown http method")
}

func (router *Router) find(method, path string) (*Route, error) {
    switch method {
    case http.MethodGet:
        if router.get != nil {
            return router.get.find(path)
        }
    case http.MethodPut:
        if router.put != nil {
            return router.put.find(path)
        }
    case http.MethodPost:
        if router.post != nil {
            return router.post.find(path)
        }
    case http.MethodHead:
        if router.head != nil {
            return router.head.find(path)
        }
    case http.MethodTrace:
        if router.trace != nil {
            return router.trace.find(path)
        }
    case http.MethodPatch:
        if router.patch != nil {
            return router.patch.find(path)
        }
    case http.MethodDelete:
        if router.delete != nil {
            return router.delete.find(path)
        }
    case http.MethodConnect:
        if router.connect != nil {
            return router.connect.find(path)
        }
    case http.MethodOptions:
        if router.options != nil {
            return router.options.find(path)
        }
    }
    return nil, ErrorRouteNotFound
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

func New() *Mux {
    mux := &Mux{}
    mux.prefix = ""
    mux.Router = &Router{}
    mux.notFound = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
        response.Header().Set("Content-Type", "text/html; charset=UTF-8")
        response.WriteHeader(http.StatusNotFound)
        response.Write([]byte("404 - Not Found"))
    })
    return mux
}

func (mux *Mux) Get(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodGet, pattern, handler)
}
func (mux *Mux) Put(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodPut, pattern, handler)
}
func (mux *Mux) Post(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodPost, pattern, handler)
}
func (mux *Mux) Head(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodHead, pattern, handler)
}
func (mux *Mux) Patch(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodPatch, pattern, handler)
}
func (mux *Mux) Trace(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodTrace, pattern, handler)
}
func (mux *Mux) Delete(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodDelete, pattern, handler)
}
func (mux *Mux) Options(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodOptions, pattern, handler)
}
func (mux *Mux) Connect(pattern string, handler RouteHandler) error {
    return mux.AddRoute(http.MethodConnect, pattern, handler)
}
func (mux *Mux) AddRoute(method string, pattern string, handler RouteHandler) error {
    pattern = path.Join(mux.prefix, pattern)
    return mux.addRoute(method, pattern, handler, mux.middleware...)
}

// HasRoute checks if a Route is present in the Mux.
// It takes two arguments: path (string) and method (string).
// If method is empty, it will look for the Route in all HTTP methods.
// It returns a boolean value indicating whether the Route is found or not, along with an error if any.
func (mux *Mux) HasRoute(path, method string) (bool, error) {
    findRoute := func(method string) (bool, error) {
        _, exception := mux.find(method, path)
        if exception == nil {
            return true, nil
        }
        if !errors.Is(exception, ErrorRouteNotFound) {
            return false, exception
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
func (mux *Mux) ServeHTTP(response http.ResponseWriter, request *http.Request) {
    url := path.Clean(request.URL.Path)
    route, exception := mux.find(request.Method, url)
    if exception != nil {
        mux.notFound.ServeHTTP(response, request)
        return
    }
    parameter, _ := ParseParameter(url, route.path)
    if parameter != "" {
        request.Header.Set(headerName, parameter)
    }
    route.ServeHTTP(response, request)
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
