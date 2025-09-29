# Usage

## normal static route

```go
package main

import (
    "log"
    "net/http"
    "github.com/zaoangod/alien"
)

func main() {
    m := alien.New()
    m.Get("/", func(response http.ResponseWriter, request *http.Request) {
        response.Write([]byte("hello world"))
    })
    log.Fatal(http.ListenAndServe(":8090", m))
}
```

## name parameter

```go
package main

import (
    "log"
    "net/http"
    "github.com/zaoangod/alien"
)

func main() {
    m := alien.New()
    m.Get("/hello/:name", func(response http.ResponseWriter, request *http.Request) {
        parameter := alien.GetParameter(request)
        w.Write([]byte(parameter.Get("name")))
    })
    log.Fatal(http.ListenAndServe(":8090", m))
}
```

## catch all parameter

```go
package main

import (
    "log"
    "net/http"
    "github.com/zaoangod/alien"
)

func main() {
    m := alien.New()
    m.Get("/hello/*name", func(w http.ResponseWriter, r *http.Request) {
        p := alien.GetParameter(r)
        w.Write([]byte(p.Get("name")))
    })
    log.Fatal(http.ListenAndServe(":8090", m))
}
```

visiting your localhost at path `/hello/my/margicl/sheeplike/ship` will print
`my/margical/sheeplike/ship`

## middleware

```go
package main

import "log"
import "net/http"
import "github.com/zaoangod/alien"

func middleware(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("hello middleware"))
    })
}

func main() {
    m := alien.New()
    m.Use(middleware)
    m.Get("/", func(_ http.ResponseWriter, _ *http.Request) {
    })
    log.Fatal(http.ListenAndServe(":8090", m))
}

```

## groups

```go
package main

import (
    "log"
    "net/http"
    "github.com/zaoangod/alien"
)

func main() {
    m := alien.New()
    g := m.Group("/home")
    m.Use(middleware)
    g.Get("/alone", func(w http.ResponseWriter, _ *http.Request) {
        w.Write([]byte("home alone"))
    })
    log.Fatal(http.ListenAndServe(":8090", m))
}
```
